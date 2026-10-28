# Escape Analysis

> **¿Quieres saber más?** Lee el gran comentario al principio de [`cmd/compile/internal/escape/escape.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/escape/escape.go), los límites de tamaño en [`cmd/compile/internal/ir/cfg.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/ir/cfg.go) y `HeapAllocReason` en [`escape/utils.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/escape/utils.go).

En los módulos anteriores redujimos el número de asignaciones pidiendo desde el principio la cantidad de memoria adecuada. Ahora vamos un paso más allá: dejamos de reservar en el heap por completo. En el lenguaje Go no existe la elección entre `new` y `malloc`. Escribes `&T{}` o `make([]byte, n)`, y es el **compilador** quien decide dónde vive esa memoria. Este módulo trata de cómo lo decide, de cómo leer sus decisiones y de cómo escribir código que le permita elegir el stack.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás por qué reservar en el stack es (casi) gratis y reservar en el heap no lo es
- Entenderás qué hace el escape analysis, y sabrás leer la salida de `-gcflags=-m` y `-m=2`
- Reconocerás los motivos habituales por los que un valor escapa: punteros devueltos, interfaces, almacenamiento en el heap, closures, tamaños desconocidos
- Conocerás los límites de tamaño del stack en Go 1.27, incluida la reserva en el stack de slices de tamaño variable (nueva en 1.25 y 1.26)
- Usarás los patrones "devuelve valores, no punteros" y "buffer proporcionado por quien llama" (`AppendX`)

## El Concepto: ¿Stack o Heap?

Cada goroutine tiene un stack. Las variables locales de una función viven en su **stack frame**, que se crea moviendo el puntero de stack cuando se llama a la función y se descarta cuando esta retorna. Reservar en el stack cuesta una resta para todo el frame, y liberar sale gratis. El GC nunca tiene que mirar un frame muerto.

![Los valores que no sobreviven a su función se quedan en el stack; los que se usan después de que la función retorne escapan al heap](images/05-stack-heap.webp)

El heap es distinto. Una asignación en el heap pasa por `runtime.mallocgc` (clases de tamaño, cachés por P, a veces locks y reserva de páginas). Desde Go 1.27, un `new(T)`/`&T{}` pequeño (hasta 80 bytes) llama en su lugar a una copia especializada por tamaño, como `mallocgcSmallScanNoHeaderSC5`: más barata, pero el mismo tipo de trabajo. Después, el garbage collector tiene que encontrar y liberar esa memoria, y todo eso es trabajo que paga tu programa (mediremos los costes del GC en [Ayudando al Garbage Collector](12-gc-pressure.md)).

Entonces, ¿por qué Go no lo pone todo en el stack? Porque un stack frame muere cuando su función retorna. Si un puntero a un valor local puede **sobrevivir al frame**, el valor tiene que ir al heap. La pasada del compilador que demuestra qué valores pueden quedarse con seguridad en el stack es el **escape analysis**.

### Cómo decide el compilador

Así describe `escape.go` su propio trabajo:

```go
// The two key invariants we have to ensure are: (1) pointers to stack
// objects cannot be stored in the heap, and (2) pointers to a stack object
// cannot outlive that object (e.g., because the declaring function
// returned and destroyed the object's stack frame, ...).
//
// We implement this with a static data-flow analysis of the AST.
// First, we construct a directed weighted graph where vertices
// (termed "locations") represent variables allocated by statements
// and expressions, and edges represent assignments between variables
// (with weights representing addressing/dereference counts).
```

Cada variable, `new`, `make`, literal compuesto, etc. se convierte en una **location**. Cada asignación se convierte en una arista, con un peso según cuántos `&` y `*` se aplican (`p = &q` es -1, `p = q` es 0, `p = *q` es 1). Hay una **location del heap** especial, y los resultados de las funciones también son locations. Después, el análisis recorre el grafo: si la dirección de una location puede llegar al heap, a un resultado o a algo que vive más tiempo, esa location escapa.

El análisis es **interprocedural**: para cada función registra cómo fluyen sus parámetros, en forma de *etiquetas de parámetro*. Eso es lo que significan los mensajes de `-m` sobre parámetros:

| Mensaje | Significado |
|---|---|
| `x does not escape` | el valor (o aquello a lo que apunta el parámetro) se queda en el frame |
| `moved to heap: x` | una *variable* local `x` vive en el heap |
| `&T{...} escapes to heap` | esa asignación es una asignación en el heap |
| `leaking param: p` | aquello a lo que apunta `p` puede llegar al heap (o sobrevivir a la llamada) |
| `leaking param content: p` | no se fuga `p` en sí, sino lo que es *alcanzable desde* él |
| `leaking param: p to result ~r0 level=0` | `p` fluye hacia el valor de retorno |

Quien llama usa esas etiquetas. Si `f(p *T)` dice "p does not escape", quien llama puede pasar `&local` y mantener `local` en su stack.

### Los sospechosos habituales

Estos son los motivos más comunes por los que un valor escapa:

1. **Devolver un puntero** a una variable local, o guardarlo en un struct que se devuelve.
2. **Almacenar en el heap**: asignar un puntero a una variable global, un map, un slice o un struct que ya está en el heap, o enviarlo por un canal.
3. **Interfaces**: convertir un valor a una interfaz y pasárselo a algo que el compilador no puede analizar por dentro, como `fmt.Printf(format string, a ...any)`. Las llamadas a través de una interfaz son opacas para el escape analysis, así que se asume que sus argumentos se fugan. ([Interfaces, Boxing y Genéricos](08-interfaces-boxing.md) trata precisamente de esto.)
4. **Closures** que capturan variables por referencia y que a su vez escapan (por ejemplo, porque se pasan a `go` o se guardan).
5. **Tamaño**: demasiado grande, o desconocido en tiempo de compilación.

### Límites de tamaño en Go 1.27

Incluso un valor que no escapa va al heap si es demasiado grande. Los límites están en `ir/cfg.go`:

```go
// MaxStackVarSize is the maximum size variable which we will allocate on the stack.
// This limit is for explicit variable declarations like "var x T" or "x := ...".
MaxStackVarSize = int64(128 * 1024)

// MaxImplicitStackVarSize is the maximum size of implicit variables that we will allocate on the stack.
//   p := new(T)          allocating T on the stack
//   p := &T{}            allocating T on the stack
//   s := make([]T, n)    allocating [n]T on the stack
//   s := []byte("...")   allocating [n]byte on the stack
MaxImplicitStackVarSize = int64(64 * 1024)
```

¿Y qué pasa con `make([]byte, n)` cuando `n` es una variable? Durante mucho tiempo la respuesta fue "siempre al heap". **Go 1.25 cambió eso.** Si el slice no escapa, el compilador ahora reserva un pequeño buffer en el stack y lo usa cuando el tamaño cabe (del comentario en `escape/utils.go`):

```go
// For non-constant sizes, we do a hybrid approach:
//
// if cap <= K {
//     var backing [K]E
//     s = backing[:len:cap]
// } else {
//     s = makeslice(E, len, cap)
// }
```

`K` es 32 *bytes* dividido entre el tamaño del elemento (`Debug.VariableMakeThreshold = 32` en `base/flag.go`). Go 1.26 extiende la misma idea a `append`: el primer crecimiento de un slice que no escapa puede usar un buffer de 32 bytes en el stack, y un slice que se construye con `append` y luego se devuelve (su única vía de escape) se mueve al heap una sola vez, en el `return` (`cmd/compile/internal/slice/slice.go` y `runtime.moveSlice`). El laboratorio incluye una demostración:

```bash
cd labs
go run ./05-escape-analysis/cmd/stackmake
```

```
n=   8   make([]byte, n): 0 allocs   make([]byte, 1024): 0 allocs
n=  32   make([]byte, n): 0 allocs   make([]byte, 1024): 0 allocs
n=  33   make([]byte, n): 1 allocs   make([]byte, 1024): 0 allocs
n=  64   make([]byte, n): 1 allocs   make([]byte, 1024): 0 allocs
n=1024   make([]byte, n): 1 allocs   make([]byte, 1024): 0 allocs
```

`-m` informa de las dos llamadas a `make` como `does not escape`. Ahí está la trampa: **"does not escape" no siempre significa "sin asignación"**. Un `make` que no escapa y cuyo tamaño se conoce en tiempo de ejecución sigue yendo al heap por encima de 32 bytes. Un tamaño constante se queda en el stack hasta los 64 KiB.

## El Laboratorio

```
labs/05-escape-analysis/
  before.go         implementaciones ingenuas (referencia, no lo edites)
  after.go          tu copia: haz que esta sea rápida
  solution.go       solución de referencia (se compila con -tags solution)
  lab_test.go       test de corrección + benchmarks
  cmd/stackmake/    el programa de demostración de arriba
```

Tres funciones procesan un pequeño formato de métricas en texto (`name value timestamp`). Cada una reserva memoria por un motivo distinto:

| Función | Qué escapa, y por qué |
|---|---|
| `Sum(lines)` | `parseSample` devuelve un `*Sample` |
| `Encode(dst, samples)` | `fmt.Appendf` recibe `...any`, y los argumentos escapan |
| `CountKnown(known, names)` | `normalize` crea un buffer de tamaño desconocido y devuelve un string nuevo |

### Paso 1: Mide la línea base

```bash
make bench LAB=05
```

Mira la tabla de `allocs/op`: cada benchmark procesa 1.000 elementos, así que verás aproximadamente 1.000 (`Sum`, `CountKnown`) y 3.000 (`Encode`) asignaciones por operación.

### Paso 2: Pregúntale al compilador

```bash
cd labs
go build -gcflags=-m ./05-escape-analysis/ 2>&1 | grep before.go
```

```
05-escape-analysis/before.go:22:24: leaking param: line
05-escape-analysis/before.go:39:9: &Sample{...} escapes to heap
05-escape-analysis/before.go:43:16: leaking param content: lines
05-escape-analysis/before.go:57:19: leaking param: dst to result ~r0 level=0
05-escape-analysis/before.go:57:31: leaking param content: samples
05-escape-analysis/before.go:59:20: ... argument does not escape
05-escape-analysis/before.go:59:41: s.Name escapes to heap
05-escape-analysis/before.go:59:49: s.Value escapes to heap
05-escape-analysis/before.go:59:58: s.Time escapes to heap
05-escape-analysis/before.go:65:22: name does not escape
05-escape-analysis/before.go:66:13: make([]byte, len(name)) does not escape
05-escape-analysis/before.go:77:16: string(buf) escapes to heap
05-escape-analysis/before.go:85:27: inlining call to normalizeBefore
05-escape-analysis/before.go:85:27: make([]byte, len(name)) does not escape
05-escape-analysis/before.go:85:27: string(buf) does not escape
```

Vamos a leerlo:

- **Línea 39**: el `*Sample` escapa porque se devuelve. `line` "se fuga" porque `Name` es un substring de `line` guardado en ese `Sample` del heap. Un parámetro de tipo *string* que se fuga no cuesta nada por sí mismo (no se hace ninguna copia); solo significa que los bytes del string tienen que seguir vivos.
- **Línea 59**: el slice `...any` se queda en el stack (`... argument does not escape`), pero cada valor empaquetado (boxing) en él escapa: `fmt` guarda sus argumentos en sitios que el compilador no puede seguir. Tres valores empaquetados, tres asignaciones por muestra.
- **Líneas 66/85**: se ha hecho inlining de `normalizeBefore` dentro de `CountKnownBefore` (más sobre inlining en el siguiente módulo), y ahí `string(buf)` solo se usa como clave de map, así que no escapa. Pero `make([]byte, len(name))` tiene un tamaño desconocido en tiempo de compilación, y nuestros nombres miden unos 48 bytes, más que el buffer de 32 bytes del stack. "Does not escape", y aun así una asignación en el heap por nombre.

¿Quieres el *porqué* de una línea concreta? `-m=2` imprime el camino del flujo de datos:

```bash
go build -gcflags=-m=2 ./05-escape-analysis/ 2>&1 | grep -A3 'before.go:39:9: &Sample{...} escapes to heap in'
```

```
05-escape-analysis/before.go:39:9: &Sample{...} escapes to heap in parseSampleBefore:
05-escape-analysis/before.go:39:9:   flow: ~r0 ← &{storage for &Sample{...}}:
05-escape-analysis/before.go:39:9:     from &Sample{...} (spill) at 05-escape-analysis/before.go:39:9
05-escape-analysis/before.go:39:9:     from return &Sample{...}, true (return) at 05-escape-analysis/before.go:39:2
```

> **Consejo:** la salida de `-m` es por paquete y puede ser larga. Fíltrala con `grep` por archivo o por línea, y recuerda que la misma línea puede aparecer dos veces: una en la propia función y otra donde se ha hecho inlining.

### Paso 3: Arréglalo

Edita `labs/05-escape-analysis/after.go`:

1. **`parseSampleAfter`**: devuelve `(Sample, bool)` en lugar de `(*Sample, bool)`. Copiar un struct de 40 bytes es barato. Devolver por valor significa que el resultado se escribe en el frame de quien llama.
2. **`EncodeAfter`**: elimina `fmt.Appendf` y escribe los campos con `append` y `strconv.AppendFloat(dst, v, 'g', -1, 64)` / `strconv.AppendInt`. Estas funciones reciben tipos concretos, así que no se empaqueta nada.
3. **`CountKnownAfter`**: convierte `normalize` en un helper de estilo append, `appendNormalized(dst []byte, name string) []byte`, y llámalo con un buffer de tamaño fijo en el stack: `var scratch [128]byte` y `appendNormalized(scratch[:0], name)`. El array tiene un tamaño constante, así que vive en el stack, y `append` solo se va al heap si un nombre mide más de 128 bytes. Después busca con `known[string(key)]`: el compilador reconoce `m[string(b)]` y no reserva un string para ello. Dentro del helper, copia el nombre entero con un solo `append(dst, name...)` y reescribe la copia en su sitio: hacer append byte a byte comprueba la capacidad en cada byte, y en Go 1.27 ese bucle es más lento que la pequeña asignación que intentas quitar.

Compruébalo con `go test ./05-escape-analysis/` y vuelve a ejecutar `-gcflags=-m`: las líneas de `&Sample{...}`, `s.Name`/`s.Value`/`s.Time` y `string(buf)` de `after.go` deberían haber desaparecido (las líneas `append escapes to heap` que quedan son `dst` creciendo, que es el buffer del llamador).

### Paso 4: Compara

```bash
make bench LAB=05
```

Estos son los resultados con la solución de referencia (`make solution LAB=05`) en un AMD Ryzen AI MAX+ 395:

```
              │    before     │               after                │
              │    sec/op     │   sec/op     vs base               │
Sum-32           43.19µ ±  4%   32.45µ ± 5%  -24.87% (p=0.000 n=8)
Encode-32       106.47µ ±  5%   34.79µ ± 1%  -67.33% (p=0.000 n=8)
CountKnown-32    46.10µ ± 12%   33.21µ ± 4%  -27.96% (p=0.000 n=8)
geomean          59.63µ         33.47µ       -43.87%

              │    before    │                  after                  │
              │     B/op     │    B/op      vs base                    │
Sum-32          31.22Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)
Encode-32       31.29Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)
CountKnown-32   46.88Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)
geomean         35.78Ki                     ?                      ¹ ²
¹ summaries must be >0 to compute geomean
² ratios must be >0 to compute geomean

              │   before    │                  after                  │
              │  allocs/op  │  allocs/op   vs base                    │
Sum-32           999.0 ± 0%      0.0 ± 0%  -100.00% (p=0.000 n=8)
Encode-32       2.999k ± 0%   0.000k ± 0%  -100.00% (p=0.000 n=8)
CountKnown-32   1.000k ± 0%   0.000k ± 0%  -100.00% (p=0.000 n=8)
geomean         1.442k                     ?                      ¹ ²
¹ summaries must be >0 to compute geomean
² ratios must be >0 to compute geomean
```

## Entendiendo los Resultados

**Sum** muestra el precio de una sola asignación pequeña en el heap: un `Sample` de 40 bytes por línea (48 bytes tras redondear a una clase de tamaño). El parseo sigue dominando el tiempo (`strconv.ParseFloat` no es barato), y aun así eliminar la asignación es una mejora sólida. Y hay una parte que no se ve en este benchmark: cada uno de esos objetos es trabajo para el GC más adelante.

**Encode** es la mayor mejora. `fmt` es cómodo, pero cada llamada parsea el string de formato en tiempo de ejecución y empaqueta cada argumento. Las funciones `strconv.Append*` son lo que `fmt` usa por dentro de todas formas, sin el parseo del formato ni el boxing.

**CountKnown** es la sutileza de Go 1.25+. Con nombres de 32 bytes o menos, el código "before" ya habría estado libre de asignaciones. Los nombres reales (como los nombres de métricas de Prometheus) suelen ser más largos. Un array de tamaño fijo es la salida clásica, y lo encontrarás por toda la librería estándar (`strconv` y `fmt` usan internamente buffers del estilo `[64]byte`). También recuerda que quitar una asignación solo compensa si lo que la sustituye hace menos trabajo: con las asignaciones pequeñas más rápidas de Go 1.27, una versión de la solución que hace `append` byte a byte es *más lenta* que "before" (medimos un +12%), mientras que la copia con un solo `append` es un ~28% más rápida.

### El patrón `AppendX`

`EncodeBefore` y `EncodeAfter` tienen la misma firma, `func(dst []byte, ...) []byte`. No es casualidad: es la forma que tiene la librería estándar de evitar asignaciones a través de la frontera de una API (`strconv.AppendInt`, `time.Time.AppendFormat`, `binary.AppendUvarint`, `fmt.Appendf`, `utf8.AppendRune`...). La función nunca reserva un buffer propio. Es **quien llama** quien decide de dónde sale la memoria: un buffer reutilizado, un array en el stack (`buf[:0]`) o `nil` si le da igual. Una función que devuelve un `[]byte` o un `string` nuevo le quita esa elección.

La misma idea se aplica a los readers: `io.Reader.Read(p []byte)` recibe el buffer de quien llama en lugar de devolver uno nuevo.

## Puntos Clave

- Reservar en el stack es prácticamente gratis; reservar en el heap cuesta tiempo de `mallocgc` **y** tiempo de GC
- El compilador decide con el **escape analysis**, un grafo de flujo de datos de locations y asignaciones
- `-gcflags=-m` te dice qué escapa, y `-m=2` te dice por qué
- Escapes habituales: punteros devueltos, almacenamiento en el heap, argumentos de tipo interfaz (`fmt`), closures que escapan, tamaños demasiado grandes o desconocidos
- Desde Go 1.25, un `make` que no escapa y cuyo tamaño se conoce en tiempo de ejecución solo se queda en el stack hasta **32 bytes**. Desde 1.26, `append` obtiene el mismo buffer de 32 bytes
- "does not escape" ≠ "no reserva memoria": confírmalo siempre con `allocs/op`
- Devuelve los structs pequeños por valor, y deja que quien llama proporcione los buffers (`AppendX(dst, ...)`)

## Para Ir Más Allá (opcional)

1. Vuelve a hacer que `parseSampleAfter` devuelva un puntero, pero haz que el `Sample` sea una variable local (`var s Sample; ...; return &s, true`). ¿Qué dice ahora `-m`? (Pista: `moved to heap: s`.)
2. Acorta los nombres de `testNames` a menos de 32 bytes y vuelve a ejecutar `CountKnown`. ¿Qué pasa con las asignaciones de "before"? ¿Por qué?
3. Ejecuta la demostración con `go run -gcflags='-d=variablemakethreshold=0' ./05-escape-analysis/cmd/stackmake`. Esto pone a 0 el umbral de 32 bytes y desactiva la optimización de Go 1.25, así que verás lo que hacían las versiones antiguas de Go. Prueba también con `=64`, y después piensa por qué el equipo de Go no eligió simplemente un número grande (pista: cada `make` de ese tipo reserva esa cantidad de stack, se use o no).
