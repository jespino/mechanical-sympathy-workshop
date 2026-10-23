# Strings y Bytes

> **¿Quieres saber más?** Lee [`runtime/string.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/string.go) (busca `slicebytetostring`, `stringtoslicebyte`, `concatstrings` y `tmpBuf`), los sitios donde el compilador se salta la copia en [`cmd/compile/internal/walk/order.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/walk/order.go), y [`strings/builder.go`](https://github.com/golang/go/blob/go1.27.0/src/strings/builder.go).

Los strings están por todas partes: líneas de log, cabeceras HTTP, claves de maps, JSON, nombres de métricas. Go hace que sean fáciles de usar, y esa facilidad esconde copias. Cada conversión entre `string` y `[]byte` puede asignar memoria y copiar, y cada `+` construye un string completamente nuevo. A veces el compilador elimina la copia por ti, y a veces no puede. En este módulo veremos cómo se representan los strings, qué cuesta cada conversión (medido, no adivinado) y cómo construir y formatear texto sin generar basura.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás qué es la cabecera de un string y por qué la inmutabilidad obliga a copiar en las conversiones
- Conocerás las funciones del runtime que hay detrás de `string(b)`, `[]byte(s)` y `+`, y el buffer de 32 bytes en el stack que pueden usar
- Sabrás exactamente qué conversiones convierte el compilador en reinterpretaciones gratuitas, verificado con `-benchmem`
- Sustituirás bucles con `+=` por `strings.Builder` + `Grow`, y `fmt.Sprintf` por `strconv.Append*`
- Evitarás la asignación escondida en `m[string(b)]++`
- Sabrás qué hacen `unsafe.String` y `unsafe.StringData`, y por qué casi nunca deberías necesitarlos

## El Concepto: Bytes Inmutables

### Dos cabeceras

Un string es una cabecera de dos palabras: un puntero y una longitud (`stringStruct` en `runtime/string.go`). Un slice tiene una tercera palabra, la capacidad:

```go
type stringStruct struct {   type slice struct {
    str unsafe.Pointer           array unsafe.Pointer
    len int                      len   int
}                                cap   int
                             }
```

![La cabecera de un string tiene puntero y longitud; la de un slice añade capacidad. Convertir entre ellos copia los bytes](images/04-headers.webp)

La diferencia importante no es la palabra extra. Es que **los bytes de un string no pueden cambiar nunca**. Eso es lo que hace que sea seguro compartirlos: los substrings (`s[3:10]`) y las asignaciones solo copian la cabecera, nunca los bytes, y un string puede ser clave de un map o pasarse entre goroutines sin locks.

La otra cara: un `[]byte` *sí* puede cambiar. Así que convertir entre ellos tiene que copiar; si no, alguien podría modificar los bytes de un "string" que otros están usando.

### Lo que hace el runtime

`string(b)` se compila como una llamada a `slicebytetostring`:

```go
func slicebytetostring(buf *tmpBuf, ptr *byte, n int) string {
    if n == 0 {
        return ""
    }
    ...
    if n == 1 {
        p := unsafe.Pointer(&staticuint64s[*ptr])
        ...
        return unsafe.String((*byte)(p), 1)
    }

    var p unsafe.Pointer
    if buf != nil && n <= len(buf) {
        p = unsafe.Pointer(buf)
    } else {
        p = mallocgc(uintptr(n), nil, false)
    }
    memmove(p, unsafe.Pointer(ptr), uintptr(n))
    return unsafe.String((*byte)(p), n)
}
```

Aquí se ven tres caminos rápidos:

- Los strings vacíos y **los de un byte nunca asignan memoria**: los bytes sueltos apuntan a una tabla estática, `staticuint64s`.
- `buf` es un `tmpBuf`, un array de 32 bytes (`const tmpStringBufSize = 32`, "There is no fundamental theory behind this number"). El compilador pasa uno, en el stack del llamante, **cuando el escape analysis demuestra que el resultado no escapa**. Los strings cortos y locales se copian, pero no se asignan.
- Todo lo demás: `mallocgc` + `memmove`.

`[]byte(s)` es `stringtoslicebyte`, la imagen especular (con el mismo truco de `tmpBuf`), y `a + b + c` es `concatstrings`, que suma las longitudes, asigna una vez (o usa `tmpBuf`) y copia cada operando. Una concatenación es una asignación, pero un `+=` en un bucle es una asignación **por iteración**, y cada una copia todo lo acumulado hasta ese momento.

### Cuándo se salta la copia el compilador

La copia solo hace falta si alguien pudiera observar que los bytes cambian. Cuando el compilador puede demostrar que nadie puede, emite `OBYTES2STRTMP` / `OSTR2BYTESTMP`: una "conversión" que simplemente reutiliza el puntero del otro valor. Los casos están en `walk/order.go`, `walk/switch.go`, las reglas de SSA y (para `[]byte(s)`) el escape analysis:

- **Búsquedas en maps**: `m[string(b)]`. De `mapKeyReplaceStrConv`: "the allocation of backing bytes for the string can be avoided by reusing the []byte backing array"
- **Comparaciones**: `string(b) == "..."`, `<`, etc. "String comparison does not memorize the strings for later use, so it is safe"
- **`switch string(b)`**, cuando ningún caso tiene efectos secundarios
- **Operandos de una concatenación**, pero solo cuando la concatenación incluye también un literal no vacío: `"<" + string(b) + ">"` (la comprobación `haslit && hasbyte`). El resultado sigue siendo un string nuevo
- **`for i, c := range []byte(s)`**: "It is safe because the storage cannot be mutated"
- **`[]byte(s)` que nunca se modifica** (Go 1.22+). El escape analysis registra si los bytes pueden escribirse: "If the result of a string->[]byte conversion is never mutated, then it can simply reuse the string's memory directly." `-gcflags=-m` imprime `zero-copy string->[]byte conversion` cuando se aplica

Vamos a comprobarlo todo. El laboratorio incluye un programa que mide cada caso con `testing.Benchmark`, usando funciones auxiliares `//go:noinline` para medir la conversión y nada más:

```bash
cd labs
go run ./04-strings-bytes/cmd/conversions
```

```
case                                ns/op     B/op  allocs/op
string(b), result escapes           12.27       48          1
string(b), local, 13 bytes           3.74        0          0
string(b), local, 44 bytes          11.14       48          1
string(b), 1 byte                    2.83        0          0
m[string(b)] lookup                  8.10        0          0
m[string(b)]++                      29.22       48          1
string(b) == "..."                   3.89        0          0
switch string(b)                     4.72        0          0
"<" + string(b) + ">"               18.71       48          1
s + string(b)                       29.89      144          2
for range []byte(s)                  8.00        0          0
[]byte(s), read only                 1.83        0          0
[]byte(s), mutated                  12.65       48          1
bytes.IndexByte([]byte(s))          15.91       48          1
[]byte(s), result escapes           13.09       48          1
```

(AMD Ryzen AI MAX+ 395. Los strings de 44 bytes ocupan una clase de tamaño de 48 bytes.) Algunos resultados merecen una segunda mirada:

- **El límite de `tmpBuf` es real**: la misma conversión local es gratis con 13 bytes y asigna con 44.
- **`m[string(b)]` es gratis, `m[string(b)]++` no.** `++` es una asignación: si la clave es nueva, el map tiene que guardarla, así que el compilador debe crear un string de verdad, siempre, aunque la clave ya exista.
- **`s + string(b)` cuesta dos asignaciones**, mientras que `"<" + string(b) + ">"` cuesta una: sin un operando literal, la conversión no se optimiza.
- **`bytes.IndexByte([]byte(s))` copia.** La regla de cero copias funciona a través de llamadas a funciones Go, porque el escape analysis sabe qué hacen con sus parámetros. `bytes.IndexByte` está implementada en ensamblador, así que el compilador debe suponer que podría escribir. (Para este caso concreto, usa directamente `strings.IndexByte`.)

### Construir strings: `strings.Builder`

`strings.Builder` es un `[]byte` al que vas añadiendo datos, más un truco en `String()`:

```go
func (b *Builder) String() string {
    return unsafe.String(unsafe.SliceData(b.buf), len(b.buf))
}
```

Sin copia: el Builder entrega su buffer como los bytes del string. Eso solo es seguro porque el Builder nunca modifica bytes que ya ha escrito (solo añade, y `grow` copia a un array nuevo), y porque `copyCheck` lanza un panic si copias por valor un Builder no vacío. Sin `Grow`, el buffer sigue creciendo como un slice (`grow` asigna `2*cap + n`), así que si puedes calcular el tamaño final, llama primero a `Grow(n)` y tendrás exactamente una asignación.

### Formatear números: `fmt` frente a `strconv`

`fmt.Sprintf("%s %g %d\n", name, value, ts)` es cómodo, pero analiza la cadena de formato en tiempo de ejecución, recibe sus argumentos como `...any` (así que cada valor que no es un puntero se **empaqueta** en una interfaz, lo que normalmente asigna memoria) y devuelve un string nuevo. `strconv.AppendInt(dst, v, 10)`, `strconv.AppendFloat(dst, v, 'g', -1, 64)` y `strconv.AppendQuote` escriben directamente en un slice de bytes que es tuyo. Cuando `dst` tiene sitio, no asignan nada.

### `unsafe.String` y `unsafe.StringData`

Desde Go 1.20, `unsafe.String(ptr, len)` construye un string a partir de un puntero y una longitud, y `unsafe.StringData(s)` devuelve el puntero que hay detrás de un string. Junto con `unsafe.Slice`/`unsafe.SliceData`, permiten convertir sin copiar, exactamente como hace `strings.Builder` por dentro. La trampa: **asumes el trabajo del compilador de demostrar que los bytes nunca cambian.** Si conviertes así un `[]byte` en string y después escribes en ese slice (o el buffer del que venía se reutiliza, como pasa con `bufio` o con los buffers de red), has creado un string que cambia. Eso rompe las claves de los maps y provoca bugs que aparecen muy lejos de donde se introdujeron. Recurre a ello solo cuando un perfil demuestre que la copia importa, la vida del buffer esté completamente bajo tu control y hayas comprobado antes que no te cubre ya alguno de los casos gratuitos del compilador.

## El Laboratorio

```
labs/04-strings-bytes/
  before.go           implementaciones ingenuas (línea base, no lo edites)
  after.go            tu copia: haz que esta sea rápida
  solution.go         solución de referencia (se compila con -tags solution)
  lab_test.go         test de corrección + benchmarks
  cmd/conversions/    la tabla de conversiones de arriba
```

Tres funciones de un pequeño exportador de métricas:

| Función | Qué hace | El problema |
|---|---|---|
| `RenderLabels(labels)` | Genera `{env="prod",host="web-1",...}` | `+=` en un bucle |
| `AppendSamples(dst, samples)` | Añade líneas `name value timestamp\n` a un buffer | `fmt.Sprintf` por línea |
| `CountTokens(tokens [][]byte)` | Cuenta tokens de tipo slice de bytes en un `map[string]int` | `counts[string(t)]++` |

### Paso 1: Mide la línea base

```bash
make bench LAB=04
```

Fíjate sobre todo en `allocs/op`: 20 para generar 10 labels, unas 4 por línea en `AppendSamples` y más o menos una por token en `CountTokens`.

### Paso 2: Busca las pruebas

Pregúntale al compilador qué opina de las conversiones de `before.go`:

```bash
cd labs
go build -gcflags=-m ./04-strings-bytes/ 2>&1 | grep before.go
```

```
04-strings-bytes/before.go:28:32: l.Name + "=\"" + l.Value + "\"" escapes to heap
04-strings-bytes/before.go:38:38: s.Name escapes to heap
04-strings-bytes/before.go:38:46: s.Value escapes to heap
04-strings-bytes/before.go:38:55: s.Timestamp escapes to heap
04-strings-bytes/before.go:49:17: string(t) escapes to heap
```

Cada una de esas líneas es una asignación dentro del bucle. `s.Value escapes to heap` sorprende a primera vista: un `float64` no puede escapar. Lo que escapa es la *interfaz* en la que se envuelve para pasárselo a `Sprintf`.

Después pregúntale al profiler de asignaciones de dónde vienen. `-memprofilerate=1` registra todas y cada una de las asignaciones en lugar de muestrear:

```bash
go test -run '^$' -bench 'AppendSamples/impl=before' -memprofile mem.prof -memprofilerate 1 ./04-strings-bytes/
go tool pprof -sample_index=alloc_objects -disasm 'AppendSamplesBefore' 04-strings-bytes.test mem.prof | grep CALL
```

```
317000     317000     CALL runtime.convTstring(SB)   ;... before.go:38
157643     157643     CALL runtime.convT64(SB)       ;... before.go:38
159204     159204     CALL runtime.convT64(SB)       ;... before.go:38
     .     317036     CALL fmt.Sprintf(SB)           ;... before.go:38
```

Ese es el empaquetado: `convTstring` para el nombre y `convT64` para el float y para el int64, cada uno asignando memoria antes incluso de llamar a `fmt.Sprintf`. Después `Sprintf` asigna el resultado. Cuatro asignaciones por línea. Borra `mem.prof` y `04-strings-bytes.test` cuando termines.

### Paso 3: Arréglalo

Edita `labs/04-strings-bytes/after.go`:

1. **`RenderLabelsAfter`**: calcula la longitud final (`2` por las llaves, más `len(name) + len(value) + 4` por label para el `=`, las dos comillas y la coma), haz `Grow` de un `strings.Builder` a ese tamaño y escribe las piezas con `WriteString` / `WriteByte`.
2. **`AppendSamplesAfter`**: añade las partes directamente: `append(dst, s.Name...)`, `append(dst, ' ')`, `strconv.AppendFloat(dst, s.Value, 'g', -1, 64)` (la misma salida que `%g`), `strconv.AppendInt(dst, s.Timestamp, 10)`, `'\n'`.
3. **`CountTokensAfter`**: haz que actualizar un token existente sea solo una búsqueda. Usa un `map[string]*int`: si `counters[string(t)]` devuelve un puntero, incrementa a través de él. Inserta (y asigna la clave) solo la primera vez que veas un token. Convierte a `map[string]int` al final.

`go test ./04-strings-bytes/` comprueba que la salida es idéntica byte a byte.

### Paso 4: Compara

```bash
make bench LAB=04
```

Resultados con la solución de referencia (`make solution LAB=04`) en un AMD Ryzen AI MAX+ 395:

```
                 │    before    │               after                │
                 │    sec/op    │   sec/op     vs base               │
RenderLabels-32    509.95n ± 7%   74.86n ± 6%  -85.32% (p=0.002 n=6)
AppendSamples-32   124.07µ ± 3%   35.83µ ± 1%  -71.12% (p=0.002 n=6)
CountTokens-32     226.76µ ± 2%   68.54µ ± 1%  -69.77% (p=0.002 n=6)

                 │    before     │                  after                   │
                 │     B/op      │     B/op      vs base                    │
RenderLabels-32      2608.0 ± 0%     224.0 ± 0%   -91.41% (p=0.002 n=6)
AppendSamples-32    78.24Ki ± 0%    0.00Ki ± 0%  -100.00% (p=0.002 n=6)
CountTokens-32     51.320Ki ± 0%   2.578Ki ± 0%   -94.98% (p=0.002 n=6)

                 │    before     │                  after                  │
                 │   allocs/op   │  allocs/op   vs base                    │
RenderLabels-32      20.000 ± 0%    1.000 ± 0%   -95.00% (p=0.002 n=6)
AppendSamples-32     3.999k ± 0%   0.000k ± 0%  -100.00% (p=0.002 n=6)
CountTokens-32     10007.00 ± 0%    43.00 ± 0%   -99.57% (p=0.002 n=6)
```

## Entendiendo los Resultados

**RenderLabels**: 20 asignaciones = 10 concatenaciones de labels + 9 comas + la llave de cierre. Cada `+=` asigna un string nuevo y copia todo el prefijo, así que los bytes crecen de forma cuadrática: 2.608 bytes asignados para producir un resultado de 209 bytes. Con `Grow` es una sola asignación de 224 bytes (la siguiente clase de tamaño), y `String()` no la copia.

**AppendSamples**: de 4.000 asignaciones a **cero**. El benchmark reutiliza un buffer entre iteraciones (`buf = fn(buf[:0], samples)`), como haría un exportador real, y una vez que ese buffer es lo bastante grande, nada en el camino de `strconv` asigna memoria. El tiempo baja 3,5 veces porque nos ahorramos analizar la cadena de formato, el empaquetado en interfaces y la maquinaria de `fmt` basada en reflexión.

**CountTokens**: 10.000 tokens pero solo 17 palabras distintas. La versión "before" asignaba una clave por cada token. La versión "after" asigna 17 claves y 17 contadores; las 9 asignaciones restantes son el almacenamiento interno de los dos maps. La pasada extra para convertir `map[string]*int` en `map[string]int` cuesta menos de lo que ahorramos. Si controlas la API, devuelve el `map[string]*int` (o un slice de contadores) y sáltate también esa pasada.

> **Regla práctica:** una conversión solo cuesta algo cuando su resultado *sobrevive a la expresión que lo usa*, o cuando *se escribe*. Buscar, comparar y leer es gratis. Guardar y modificar, no.

## Puntos Clave

- Un string es un `(ptr, len)` inmutable. Compartirlo y hacer substrings es gratis; convertir a o desde `[]byte` copia, porque uno de los lados puede modificarse
- Las conversiones y concatenaciones que no escapan y ocupan hasta 32 bytes usan un `tmpBuf` en el stack; los strings de 0 y 1 byte nunca asignan memoria
- El compilador elimina la copia en búsquedas en maps, comparaciones, `switch`, concatenaciones con un literal, `range []byte(s)` y `[]byte(s)` que nunca se escribe
- `m[string(b)]++` y `m[string(b)] = v` siempre asignan la clave. Busca primero, y guarda punteros si necesitas actualizar
- `+=` en un bucle es copia cuadrática. Usa `strings.Builder` y `Grow` al tamaño final
- `strconv.Append*` sobre un buffer reutilizado gana a `fmt.Sprintf`: sin análisis de formato, sin empaquetado, sin string nuevo
- `unsafe.String` traslada la demostración de inmutabilidad del compilador a ti. Mide primero

## Para Ir Más Allá (opcional)

1. En `RenderLabelsAfter`, quita la llamada a `Grow`. ¿Cuántas asignaciones obtienes ahora, y por qué ese número? (Pista: `grow` pide `2*cap + n`.)
2. Reescribe `AppendSamplesBefore` con `fmt.Appendf(dst, ...)` en lugar de `Sprintf`. ¿Qué asignaciones desaparecen y cuáles se quedan?
3. Ejecuta `go build -gcflags=-m ./04-strings-bytes/cmd/conversions` y empareja cada mensaje `zero-copy string->[]byte conversion` y `does not escape` con una fila de la tabla.
