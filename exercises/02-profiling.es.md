# Profiling 101

Un benchmark te dice *lo rápido* que es algo. No te dice *por qué*. Para eso necesitas un profiler, y Go tiene uno integrado en el runtime: sin instrumentación, sin recompilar, basta con un flag. En este módulo veremos cómo recogen sus datos los profilers de CPU y de heap, porque saberlo te dice hasta qué punto puedes fiarte de ellos. Después usaremos `go tool pprof` para encontrar un punto caliente que no está donde te imaginarías.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Entenderás cómo muestrea tu programa el profiler de CPU (una instantánea de la pila 100 veces por segundo)
- Entenderás cómo muestrea las asignaciones el profiler de heap (más o menos una cada 512 KB) y la diferencia entre `alloc_space` e `inuse_space`
- Obtendrás perfiles desde benchmarks (`-cpuprofile`, `-memprofile`) y desde programas (`runtime/pprof`, `net/http/pprof`)
- Sabrás leer la salida de `pprof`: `flat` frente a `cum`, `-top`, `-list` y el flame graph
- Encontrarás y arreglarás un punto caliente escondido tras una función auxiliar

## El Concepto: Profilers de Muestreo

Los profilers de Go son profilers de **muestreo**. No registran cada llamada a función, algo que sería demasiado lento. En su lugar toman una instantánea cada cierto tiempo y cuentan cuántas caen en cada sitio. Más instantáneas significa más tiempo (o más bytes).

### El profiler de CPU: una instantánea 100 veces por segundo

Mientras el profiler de CPU está activo, el runtime de Go interrumpe tu programa 100 veces por segundo (cada 10 ms) y anota qué estaba haciendo en ese momento: la función que se estaba ejecutando y las funciones que la llamaron (la **pila**). Al final, `pprof` cuenta en cuántas instantáneas aparece cada función. Una función que aparece en la mitad usó más o menos la mitad del tiempo de CPU.

![El profiler de CPU interrumpe el programa 100 veces por segundo y anota el stack](images/02-sampling.webp)

El reloj que dispara esas instantáneas cuenta **tiempo de CPU**, no tiempo real, y cada hilo tiene el suyo. Qué significa esto para ti:

- **Las esperas no aparecen.** Una goroutine bloqueada en un canal, un lock o la red no está usando la CPU, así que nunca se la pilla. Go tiene otros perfiles para eso, como los de bloqueo y de mutex.
- **El total de muestras puede superar el tiempo real.** Dos hilos ocupados durante 1 s producen 2 s de muestras.
- **Es estadística, no una cuenta exacta.** 1 s de CPU te da unas 100 instantáneas, así que una función al 2% puede ser ruido.

### El profiler de heap: una muestra cada 512 KB

El profiler de heap no interrumpe tu programa. En su lugar, el allocator lleva la cuenta de cuántos bytes se han asignado y, más o menos cada 512 KB, anota la pila de la asignación que ocurre en ese momento. Después `pprof` escala las muestras para estimar los totales reales, así que aquí también los números pequeños son estimaciones. Si necesitas todas las asignaciones, `go test -memprofilerate=1` las registra todas, pero hace tu programa bastante más lento, así que úsalo solo cuando necesites números exactos.

Un perfil de heap contiene cuatro valores por pila:

| Tipo de muestra | Significado |
|---|---|
| `alloc_space` / `alloc_objects` | Todo lo asignado desde que arrancó el programa, aunque ya sea basura |
| `inuse_space` / `inuse_objects` | Lo que seguía vivo en el GC más reciente |

El perfil `heap` muestra por defecto `inuse_space`: lo que está usando memoria *ahora*, que es lo que quieres para buscar fugas. El perfil `allocs` tiene los mismos datos pero muestra por defecto `alloc_space`: todo lo que generó trabajo para el allocator y el GC, que es lo que quieres para hacer el código más rápido. `go test -memprofile` escribe el perfil `allocs`.

### Cómo obtener perfiles

Hay tres formas habituales de obtener un perfil, según lo que estés ejecutando.

La más fácil, y la que usaremos en los laboratorios, es pedírselo a `go test` mientras ejecuta un benchmark. Añade `-cpuprofile` para un perfil de CPU, o `-memprofile` para uno de heap, y dale un nombre de fichero:

```bash
go test -run '^$' -bench Foo -cpuprofile cpu.prof ./pkg/
go test -run '^$' -bench Foo -memprofile mem.prof ./pkg/
```

Cuando el código que quieres mirar no es un benchmark sino un programa normal, puedes arrancar y parar tú el profiler de CPU con el paquete `runtime/pprof`. Todo lo que se ejecute entre esas dos llamadas acaba en el fichero:

```go
f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()
```

Y de un servidor puedes sacar un perfil mientras sigue en marcha atendiendo peticiones. Importa `net/http/pprof` con el identificador en blanco: no llamas a nada del paquete, el import por sí solo añade unas cuantas páginas bajo `/debug/pprof/` a tu servidor HTTP (si usa `http.DefaultServeMux`):

```go
import _ "net/http/pprof"
```

Después apunta `go tool pprof` a esas páginas. Descarga el perfil y lo abre, igual que si fuera un fichero. El de CPU graba durante los segundos que le pidas:

```bash
go tool pprof 'http://localhost:6060/debug/pprof/profile?seconds=5'   # CPU
go tool pprof http://localhost:6060/debug/pprof/heap                   # heap
```

### Leer un perfil: `flat` frente a `cum`

Cada informe de `pprof` tiene dos números por función:

- **flat**: muestras en las que esta función estaba *en lo alto de la pila*, ejecutando su propio código
- **cum** (acumulado): muestras en las que esta función estaba *en cualquier punto de la pila*: ella misma más todo lo que llamó

![flat cuenta las muestras en las que la propia función se ejecutaba, cum incluye todo lo que llamó](images/02-flat-cum.webp)

`flat` te dice qué código quema ciclos. `cum` te dice qué *decisión* es la responsable. Un programa lento suele tener un perfil flat en el que nada destaca (un malloc aquí, una búsqueda en un map allá) mientras una función tiene un `cum` enorme. Eso normalmente significa que el problema no es un trozo de código lento, sino una decisión en esa función, como llamar a algo caro dentro de un bucle o repetir el mismo trabajo una y otra vez. Esa función es donde deberías mirar primero.

## El Laboratorio

```
labs/02-profiling/
  before.go          ReportBefore, la versión lenta (línea base, no lo edites)
  after.go           tu copia: haz que esta sea rápida
  solution.go        solución de referencia (se compila con -tags solution)
  data.go            GenerateLog, un generador de logs de acceso falsos
  lab_test.go        tests de corrección + benchmark
  cmd/logreport/     el mismo código como programa, con -cpuprofile y -http
```

`ReportBefore` recibe líneas de log de acceso como

```
2026-10-23T10:00:00Z GET /api/users/4821/orders/17 200 12ms
```

y devuelve el número de peticiones y errores por endpoint, con los IDs numéricos normalizados (`GET /api/users/:id/orders/:id`). El benchmark la ejecuta sobre 10.000 líneas. Es lenta, y tu trabajo es averiguar por qué **sin adivinar**. ¡Resiste la tentación de leer `before.go` con mucho detalle antes de perfilar!

### Paso 1: Mide la línea base

```bash
make bench LAB=02
```

Apunta `sec/op`, `B/op` y `allocs/op`. Unos 12 ms, 16 MB y 225.000 asignaciones para procesar 10.000 líneas: 22 asignaciones y 1,6 KB por línea. Eso ya debería parecerte mucho.

### Paso 2: Perfila la CPU

```bash
cd labs
go test -run '^$' -bench 'Report/impl=before' -cpuprofile cpu.prof ./02-profiling/
go tool pprof -top cpu.prof | head -20
```

```
Duration: 1.20s, Total samples = 1.59s (132.93%)
      flat  flat%   sum%        cum   cum%
     0.11s  6.92%  6.92%      0.11s  6.92%  strings.Fields
     0.07s  4.40% 11.32%      0.11s  6.92%  regexp.(*Regexp).tryBacktrack
     0.06s  3.77% 15.09%      0.06s  3.77%  runtime.(*mspan).init
     0.05s  3.14% 18.24%      0.32s 20.13%  runtime.growslice
     0.05s  3.14% 21.38%      0.41s 25.79%  runtime.mallocgc
     ...
```

La vista flat es un caos: ninguna función pasa del 7%. Mucho malloc, algo de regexp, algo de GC. Ahora ordena por acumulado, añadiendo el flag `-cum`:

```bash
go tool pprof -top -cum cpu.prof | head -16
```

```
      flat  flat%   sum%        cum   cum%
         0     0%     0%      1.27s 79.87%  labs/02-profiling.ReportBefore
         0     0%     0%      1.09s 68.55%  labs/02-profiling.normalizePathBefore
         0     0%     0%      0.79s 49.69%  regexp.MustCompile
         0     0%     0%      0.79s 49.69%  regexp.Compile (inline)
         0     0%     0%      0.79s 49.69%  regexp.compile
     0.05s  3.14%  3.14%      0.41s 25.79%  runtime.mallocgc
     0.01s  0.63%  3.77%      0.33s 20.75%  regexp/syntax.parse
         0     0% 14.47%      0.20s 12.58%  runtime.gcBgMarkWorker
```

Eso ya cuenta una historia: la mayor parte de `ReportBefore` es `normalizePathBefore`, y la mayor parte de *esta* es `regexp.MustCompile`. Mira el coste línea a línea con `-list`:

```bash
go tool pprof -list 'normalizePathBefore' cpu.prof
```

```
         .      790ms     48:	re := regexp.MustCompile(`/[0-9]+`)
         .      300ms     49:	return re.ReplaceAllString(path, "/:id")
```

Por último, abre el flame graph y encuentra lo mismo de forma visual: `go tool pprof -http=: cpu.prof` y después *View → Flame Graph*. El ancho de cada barra es su `cum`.

![El flame graph de pprof de ReportBefore: normalizePathBefore ocupa casi todo el ancho, y regexp.MustCompile la mayor parte de eso](images/02-flame-graph.webp)

`normalizePathBefore` ocupa casi todo el ancho y, dentro de ella, `regexp.MustCompile` es la barra más ancha.

### Paso 3: Confírmalo con el perfil de heap

```bash
go test -run '^$' -bench 'Report/impl=before' -memprofile mem.prof ./02-profiling/
go tool pprof -sample_index=alloc_space -top mem.prof | head -12
```

```
Showing nodes accounting for 1.33GB, 99.12% of 1.34GB total
      flat  flat%   sum%        cum   cum%
    0.49GB 36.81% 36.81%     0.49GB 36.81%  regexp/syntax.(*compiler).inst (inline)
    0.36GB 26.54% 63.34%     0.36GB 26.54%  regexp/syntax.(*parser).newRegexp (inline)
    0.14GB 10.27% 73.61%     1.19GB 88.49%  regexp.compile
  ...
    0.06GB  4.48% 86.06%     0.06GB  4.48%  strings.Fields
```

**El 88% de todos los bytes** viene de compilar la expresión regular. Todo eso es trabajo para el allocator y el GC (eso es el `gcBgMarkWorker` del perfil de CPU).

### Paso 4: Arréglalo

Edita `labs/02-profiling/after.go`. La expresión regular nunca cambia y, aun así, se compila una vez **por cada línea de log**. Compílala una sola vez, en una variable de paquete, y reutilízala (un `*regexp.Regexp` compilado es seguro para uso concurrente):

```go
var idSegment = regexp.MustCompile(`/[0-9]+`)
```

`go test ./02-profiling/` comprueba que el informe no ha cambiado.

### Paso 5: Compara

```bash
make bench LAB=02
```

Resultados con la solución de referencia (`make solution LAB=02`) en mi máquina:

```
          │    before    │               after                │
          │    sec/op    │   sec/op     vs base               │
Report-32   11.593m ± 8%   3.109m ± 2%  -73.19% (p=0.002 n=6)

          │    before     │                after                │
          │     B/op      │     B/op      vs base               │
Report-32   15.897Mi ± 0%   1.684Mi ± 0%  -89.40% (p=0.002 n=6)

          │    before    │               after                │
          │  allocs/op   │  allocs/op   vs base               │
Report-32   225.08k ± 0%   55.04k ± 0%  -75.55% (p=0.002 n=6)
```

## Entendiendo los Resultados

**3,7 veces más rápido y 9 veces menos memoria moviendo una línea.** La lección no es "las regexps son lentas". Es que el coste era invisible en el código (`normalizePath` parece inocente y diminuta) e invisible en el perfil flat, donde estaba repartido entre docenas de funciones del parser de regexps y del allocator. Solo `cum` conectó todo ese trabajo con una única decisión.

Perfila la nueva versión y el panorama cambia:

```
      flat  flat%   sum%        cum   cum%
     0.01s  0.75%  0.75%      1.15s 86.47%  labs/02-profiling.ReportAfter
         0     0%  0.75%      0.68s 51.13%  labs/02-profiling.normalizePathAfter (inline)
         0     0%  0.75%      0.68s 51.13%  regexp.(*Regexp).ReplaceAllString
     0.26s 19.55% 26.32%      0.29s 21.80%  strings.Fields
         0     0% 42.86%      0.13s  9.77%  runtime.gcBgMarkWorker
```

Ahora ejecutar la regexp es la mitad del tiempo, y `strings.Fields` (un `[]string` por línea) es lo siguiente. Así funciona el profiling en la práctica: arregla lo primero de la lista, vuelve a perfilar y para cuando lo primero sea algo que estés dispuesto a pagar.

## Puntos Clave

- Los profilers de Go **muestrean**: el de CPU unas 100 veces por segundo de tiempo de CPU, el de heap más o menos una vez cada 512 KB asignados
- El perfil de CPU muestra **tiempo de CPU, no esperas**. El total de muestras puede superar el tiempo real cuando hay varios hilos ocupados
- `alloc_space` encuentra el código que genera trabajo para el allocator y el GC; `inuse_space` encuentra lo que está reteniendo memoria
- `flat` muestra dónde se queman los ciclos; `cum` muestra quién es el responsable. Empieza con `-top -cum`, y sigue con `-list`
- Mide con un benchmark, explica con un perfil, arregla y **vuelve a perfilar**

## Para Ir Más Allá (opcional)

1. El siguiente punto caliente es `regexp.ReplaceAllString`. Sustituye la expresión regular por un bucle escrito a mano sobre los segmentos de la ruta. ¿Cuánto más rápido es? ¿Sigue siendo igual de fácil de leer?
2. Sustituye `strings.Fields` por una versión que no asigne un slice por línea (por ejemplo, `strings.Cut` tres veces). Comprueba la ganancia con `benchstat`.
3. Ejecuta `go test -bench . -memprofile mem.prof -memprofilerate 1` y compara los totales con los del perfil muestreado por defecto. ¿Cuánto se acercaba la estimación?
