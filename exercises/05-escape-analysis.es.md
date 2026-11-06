# Escape Analysis

En los módulos anteriores redujimos el número de asignaciones pidiendo desde el principio la cantidad de memoria adecuada. Ahora vamos un paso más allá: dejamos de reservar en el heap por completo. En el lenguaje Go no existe la elección entre `new` y `malloc`. Escribes `&T{}` o `make([]byte, n)`, y es el **compilador** quien decide dónde vive esa memoria. Este módulo trata de cómo lo decide, de cómo leer sus decisiones y de cómo escribir código que le permita elegir el stack.

## Contenido que Quizá Quieras Ver Primero

- [Escape Analysis in Go: Stack or Heap?](https://www.youtube.com/watch?v=BanG_wLnSfE) (vídeo de Phuong Le, en inglés): stacks, heap, `-gcflags=-m` y los límites del análisis, incluido un cambio de Go 1.27.
- [El IR](https://internals-for-interns.com/es/posts/the-go-ir/#análisis-de-escape-heap-o-stack) (artículo, Internals for Interns): cómo sigue el compilador a un valor para decidir si escapa.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Entenderás el coste de las asignaciones en el stack y en el heap
- Sabrás leer la salida del escape analysis con `-gcflags=-m` y `-m=2`
- Reconocerás las causas habituales de las asignaciones en el heap
- Reducirás asignaciones devolviendo valores, evitando el boxing y reutilizando buffers

## El Concepto: ¿Stack o Heap?

Cada goroutine tiene un **stack**. Cada llamada a una función obtiene en él su propio espacio para sus variables locales (su *stack frame*), y cuando la función retorna, ese espacio se descarta de golpe. Reservar en el stack es prácticamente gratis, liberar también, y el garbage collector nunca tiene que mirarlo.

El **heap** es donde va todo lo demás. Reservar ahí le cuesta trabajo de verdad al runtime, y después el garbage collector tiene que darse cuenta de que esa memoria ya no se usa y liberarla. Mediremos ese coste en [Ayudando al Garbage Collector](12-gc-pressure.md).

En Go no eliges entre los dos. Escribes `&T{}` o `make([]byte, n)`, y el compilador decide. La regla es sencilla:

![Los valores que no sobreviven a su función se quedan en el stack; los que se usan después de que la función retorne escapan al heap](images/05-stack-heap.webp)

- Si un valor solo se usa mientras la función se ejecuta, puede vivir en el **stack**.
- Si se puede seguir usando después de que la función retorne (por ejemplo, porque devuelves un puntero a él), tiene que **escapar** al **heap**, porque para entonces el stack frame ya no existirá.

La parte del compilador que averigua esto se llama **escape analysis**. Sigue cada puntero por tu código, también dentro de las funciones a las que llamas, para ver dónde puede acabar.

### Los sospechosos habituales

Estos son los motivos más comunes por los que un valor escapa:

1. **Devolver un puntero** a una variable local.
2. **Guardarlo en algo que vive más**: una variable global, un map, un slice o un struct que ya está en el heap, o un canal.
3. **Interfaces**: pasar un valor como interfaz a algo que el compilador no puede analizar por dentro, como `fmt.Printf(format string, a ...any)`. ([Interfaces y Boxing](08-interfaces-boxing.md) trata precisamente de esto.)
4. **Closures** que capturan variables y que a su vez se guardan, por ejemplo al pasarlas a `go`.
5. **Tamaño**: demasiado grande para el stack, o desconocido en tiempo de compilación.

### Preguntarle al compilador

`go build -gcflags=-m` imprime las decisiones del compilador. Estos son los mensajes que más verás:

| Mensaje | Significado |
|---|---|
| `x does not escape` | `x` se queda en el stack |
| `moved to heap: x` | la variable local `x` vive en el heap |
| `&T{...} escapes to heap` | esa asignación va al heap |
| `leaking param: p` | lo que apunta `p` puede sobrevivir a la llamada |

Usa `-m=2` para ver *por qué* escapa algo. Usaremos los dos en el laboratorio.

## El Laboratorio

```
labs/05-escape-analysis/
  before.go         implementaciones ingenuas (referencia, no lo edites)
  after.go          tu copia: haz que esta sea rápida
  solution.go       solución de referencia (se compila con -tags solution)
  lab_test.go       test de corrección + benchmarks
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

- **Línea 39**: devolver `*Sample` lo lleva al heap. Su campo `Name` mantiene vivos los bytes del string original sin copiarlos.
- **Línea 59**: pasar los tres campos a `fmt` como `any` provoca tres asignaciones por muestra.
- **Líneas 66/85**: la búsqueda en el map evita copiar `string(buf)`, pero el buffer temporal de bytes sigue necesitando una asignación en el heap por nombre.

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

1. **`parseSampleAfter`**: devuelve `(Sample, bool)` en lugar de un puntero para evitar que la muestra vaya al heap.
2. **`EncodeAfter`**: sustituye `fmt.Appendf` por `append`, `strconv.AppendFloat` y `strconv.AppendInt` para evitar el boxing.
3. **`CountKnownAfter`**: reutiliza un buffer `var scratch [128]byte`. Pasa `scratch[:0]` a un helper `appendNormalized(dst []byte, name string) []byte` que copie el nombre con `append(dst, name...)` y lo normalice en su sitio. Busca el resultado con `known[string(key)]`.

Ejecuta `go test ./05-escape-analysis/` para comprobar el resultado y después `-gcflags=-m` otra vez para ver qué asignaciones en el heap han desaparecido.

### Paso 4: Compara

```bash
make bench LAB=05
```

Estos son los resultados con la solución de referencia (`make solution LAB=05`) en mi máquina:

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

**Sum** evita reservar un `Sample` en el heap por cada línea al devolverlo por valor. Leer los números del texto sigue siendo lo que más tarda, pero quitar la asignación ayuda y deja menos trabajo para el recolector de basura.

**Encode** es la mayor mejora. Cada llamada a `fmt` interpreta el string de formato y envuelve los argumentos como valores de interfaz (boxing). `strconv.Append*` escribe los números directamente en el buffer de salida, evitando ese trabajo extra.

**CountKnown** evita reservar un buffer temporal por nombre usando un array de tamaño fijo. Con nombres de 32 bytes o menos, Go ya evita esa asignación, pero los nuestros son más largos. También importa cómo llenamos el buffer: copiar el nombre entero con un solo `append` es un 28% más rápido que antes, mientras que añadir byte a byte era un 12% más lento. Menos asignaciones no garantizan un código más rápido.

## Puntos Clave

- Las asignaciones en el heap añaden trabajo al runtime y al recolector de basura.
- El escape analysis ayuda al compilador a decidir dónde vive la memoria.
- Usa `-gcflags=-m` para ver sus decisiones y `-m=2` para más detalle.
- Devuelve structs pequeños por valor, evita el boxing innecesario y reutiliza buffers.
- Confirma la mejora con `allocs/op` y los tiempos del benchmark.

## Para Ir Más Allá (opcional)

1. Vuelve a hacer que `parseSampleAfter` devuelva un puntero, pero haz que el `Sample` sea una variable local (`var s Sample; ...; return &s, true`). ¿Qué dice ahora `-m`? (Pista: `moved to heap: s`.)
2. Acorta los nombres de `testNames` a menos de 32 bytes y vuelve a ejecutar `CountKnown`. ¿Qué pasa con las asignaciones de "before"? ¿Por qué?
3. En `BenchmarkEncode`, pasa `nil` en lugar de `buf[:0]` en cada iteración. ¿Cómo cambian `B/op` y `allocs/op` cuando dejas de reutilizar el buffer de salida?
