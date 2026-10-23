# Profiling 101

> **¿Quieres saber más?** Lee [`runtime/pprof/pprof.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/pprof/pprof.go) (busca `StartCPUProfile`), los temporizadores de profiling por hilo en [`runtime/os_linux.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/os_linux.go) (`setThreadCPUProfiler`, `validSIGPROF`), el manejador de señal `sigprof` en [`runtime/proc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/proc.go) y el muestreo del heap en [`runtime/malloc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/malloc.go) y [`runtime/mprof.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mprof.go).

Un benchmark te dice *lo rápido* que es algo. No te dice *por qué*. Para eso necesitas un profiler, y Go tiene uno integrado en el runtime: sin instrumentación, sin recompilar, basta con un flag. En este módulo veremos cómo recogen sus datos los profilers de CPU y de heap, porque saberlo te dice hasta qué punto puedes fiarte de ellos. Después usaremos `go tool pprof` para encontrar un punto caliente que no está donde te imaginarías.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Entenderás cómo muestrea tu programa el profiler de CPU (SIGPROF, 100 Hz, temporizadores por hilo en Linux)
- Entenderás cómo muestrea las asignaciones el profiler de heap (una cada ~512 KB) y la diferencia entre `alloc_space` e `inuse_space`
- Obtendrás perfiles desde benchmarks (`-cpuprofile`, `-memprofile`) y desde programas (`runtime/pprof`, `net/http/pprof`)
- Sabrás leer la salida de `pprof`: `flat` frente a `cum`, `-top`, `-list`, `-peek` y el flame graph
- Encontrarás y arreglarás un punto caliente escondido tras una función auxiliar

## El Concepto: Profilers de Muestreo

Los profilers de Go son profilers de **muestreo**. No registran cada llamada a función, algo que sería demasiado lento. En su lugar toman una instantánea cada cierto tiempo y cuentan cuántas caen en cada sitio. Más instantáneas significa más tiempo (o más bytes).

### El profiler de CPU: una señal 100 veces por segundo

`pprof.StartCPUProfile` fija la frecuencia en el código:

```go
// 100 Hz is a reasonable choice: it is frequent enough to
// produce useful data, rare enough not to bog down the
// system, and a nice round number to make it easy to
// convert sample counts to seconds. Instead of requiring
// each client to specify the frequency, we hard code it.
const hz = 100
```

![El profiler de CPU interrumpe el programa 100 veces por segundo y anota el stack](images/02-sampling.webp)

y llama a `runtime.SetCPUProfileRate(hz)`. A partir de ahí, el kernel entrega una señal `SIGPROF` cada 10 ms **de tiempo de CPU consumido**. El manejador de señales del runtime llama a `sigprof` (en `proc.go`), que recorre la pila de la goroutine interrumpida (hasta `maxCPUProfStack = 64` frames) y la añade a un buffer que una goroutine en segundo plano va escribiendo como perfil.

*Qué* reloj genera esas señales importa. El mecanismo clásico de Unix, `setitimer(ITIMER_PROF)`, es de todo el proceso: el kernel elige algún hilo para entregarle la señal y, con muchos hilos ocupados, se pierden muestras o caen en el sitio equivocado. En Linux, Go además crea **un temporizador por hilo** con `timer_create(CLOCK_THREAD_CPUTIME_ID)`, en `setThreadCPUProfiler`:

```go
spec.it_value.setNsec(1 + int64(cheaprandn(uint32(1e9/hz))))
spec.it_interval.setNsec(1e9 / int64(hz))

var timerid int32
var sevp sigevent
sevp.notify = _SIGEV_THREAD_ID
sevp.signo = _SIGPROF
sevp.sigev_notify_thread_id = int32(mp.procid)
ret := timer_create(_CLOCK_THREAD_CPUTIME_ID, &sevp, &timerid)
```

Cada hilo recibe una señal por cada 10 ms de *su propio* tiempo de CPU. El retardo inicial aleatorio hace que incluso un hilo que solo se ejecuta 1 ms tenga un 10% de probabilidad de salir en el perfil, en lugar de no aparecer nunca. Como los dos temporizadores pueden estar activos, `validSIGPROF` comprueba el origen de la señal (`_SI_TIMER` frente a `_SI_KERNEL`) para no contar dos veces el mismo tiempo de CPU.

Lo que esto significa para ti:

- **Las muestras miden tiempo de CPU, no tiempo real.** Una goroutine esperando en un canal, un lock o la red no consume CPU, así que no aparece. (Para eso usa los profilers de block, mutex o el trace. Para goroutines bloqueadas *para siempre*, en un canal o lock que ya ninguna goroutine en ejecución puede alcanzar, Go 1.27 añade el perfil `goroutineleak`, también servido en `/debug/pprof/goroutineleak`.)
- **El total de muestras puede superar el tiempo real.** Dos hilos ocupados durante 1 s producen 2 s de muestras. Más abajo verás `Duration: 1.20s, Total samples = 1.59s (132.93%)`: lo que sobra es sobre todo el garbage collector trabajando en otros hilos.
- **Es estadístico.** 1 s de CPU te da unas ~100 muestras. Una función al 2% puede ser ruido. Si necesitas detalle, perfila durante más tiempo.

### El profiler de heap: una muestra cada 512 KB

Las asignaciones se muestrean por bytes, no por tiempo. De `mprof.go`:

```go
// The profiler aims to sample an average of
// one allocation per MemProfileRate bytes allocated.
var MemProfileRate int = 512 * 1024
```

Cada `mcache` lleva una cuenta atrás, `nextSample`. Cuando una asignación la cruza, `profilealloc` registra la pila de esa asignación y saca una nueva cuenta atrás de una distribución exponencial con media `MemProfileRate` (`nextSample` en `malloc.go`), de forma que los puntos de muestreo se reparten al azar en lugar de caer cada N objetos. Después `pprof` escala cada muestra para estimar los totales reales. Igual que en el perfil de CPU, los números pequeños son estimaciones: pon `runtime.MemProfileRate = 1` (o `go test -memprofilerate=1`) para registrar todas las asignaciones, a cambio de un coste.

Un perfil de heap contiene cuatro valores por pila:

| Tipo de muestra | Significado |
|---|---|
| `alloc_space` / `alloc_objects` | Todo lo asignado desde que arrancó el programa, aunque ya sea basura |
| `inuse_space` / `inuse_objects` | Lo que seguía vivo en el GC más reciente |

El perfil `heap` muestra por defecto `inuse_space` (lo que está usando memoria *ahora*: buscar fugas), y el perfil `allocs` son los mismos datos mostrando por defecto `alloc_space` (lo que genera trabajo para el allocator y el GC: buscar velocidad). `go test -memprofile` escribe el perfil `allocs`, después de un `runtime.GC()` para "materialize all statistics".

### Cómo obtener perfiles

Desde benchmarks, que es lo que usaremos en los laboratorios:

```bash
go test -run '^$' -bench Foo -cpuprofile cpu.prof ./pkg/
go test -run '^$' -bench Foo -memprofile mem.prof ./pkg/
```

Desde un programa, con `runtime/pprof`:

```go
f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()
```

Desde un servidor en marcha, importando `net/http/pprof` por su efecto secundario. Registra handlers bajo `/debug/pprof/` en `http.DefaultServeMux`:

```go
import _ "net/http/pprof"
```

```bash
go tool pprof 'http://localhost:6060/debug/pprof/profile?seconds=5'   # CPU
go tool pprof http://localhost:6060/debug/pprof/heap                   # heap
```

### Leer un perfil: `flat` frente a `cum`

Cada informe de `pprof` tiene dos números por función:

- **flat**: muestras en las que esta función estaba *en lo alto de la pila*, ejecutando su propio código
- **cum** (acumulado): muestras en las que esta función estaba *en cualquier punto de la pila*: ella misma más todo lo que llamó

![flat cuenta las muestras en las que la propia función se ejecutaba, cum incluye todo lo que llamó](images/02-flat-cum.webp)

`flat` te dice qué código quema ciclos. `cum` te dice qué *decisión* es la responsable. Un programa lento suele tener un perfil flat en el que nada destaca (un malloc aquí, una búsqueda en un map allá) mientras una función tiene un `cum` enorme. Los comandos que usarás:

| Comando | Muestra |
|---|---|
| `-top` | Funciones ordenadas por flat. Añade `-cum` para ordenar por acumulado |
| `-list Regexp` | El código fuente anotado de las funciones que coinciden, con el tiempo por línea |
| `-peek Regexp` | Quién llama a las funciones que coinciden y a quién llaman ellas |
| `-http=:` | Interfaz web con un grafo, un flame graph (View → Flame Graph) y vistas del código |

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

La vista flat es un caos: ninguna función pasa del 7%. Mucho malloc, algo de regexp, algo de GC. (En Go 1.27 el coste del allocator está aún más repartido: más abajo encontrarás rutinas especializadas por tamaño como `runtime.mallocgcSmallScanNoHeaderSC5`, una por clase de tamaño; mira el [módulo 03](03-preallocation.md).) Ahora ordena por acumulado:

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

Eso ya cuenta una historia: la mayor parte de `ReportBefore` es `normalizePathBefore`, y la mayor parte de *esta* es `regexp.MustCompile`. Confirma quién la llama:

```bash
go tool pprof -peek 'regexp.MustCompile$' cpu.prof
```

```
                                             0.79s   100% |   labs/02-profiling.normalizePathBefore
         0     0%     0%      0.79s 49.69%                | regexp.MustCompile
                                             0.79s   100% |   regexp.Compile (inline)
```

Y mira el coste línea a línea:

```bash
go tool pprof -list 'normalizePathBefore' cpu.prof
```

```
         .      790ms     48:	re := regexp.MustCompile(`/[0-9]+`)
         .      300ms     49:	return re.ReplaceAllString(path, "/:id")
```

Por último, abre el flame graph y encuentra lo mismo de forma visual: `go tool pprof -http=: cpu.prof` y después *View → Flame Graph*. El ancho de cada barra es su `cum`.

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

**El 88% de todos los bytes** viene de compilar la expresión regular. Prueba también `-sample_index=inuse_space`: está casi vacío, porque todo eso es basura de vida corta. No es una fuga, solo mucho trabajo para el allocator y el GC (eso es el `gcBgMarkWorker` del perfil de CPU).

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

Resultados con la solución de referencia (`make solution LAB=02`) en un AMD Ryzen AI MAX+ 395:

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

Borra los perfiles cuando termines: `rm -f cpu.prof mem.prof 02-profiling.test`. (Cuando pasas un flag de profiling, `go test` deja el binario de test junto al perfil.)

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

### Perfilar un programa en lugar de un benchmark

El mismo código está envuelto en un pequeño programa. Ejecútalo con `-cpuprofile`, que usa `runtime/pprof` directamente:

```bash
cd labs
go run ./02-profiling/cmd/logreport -cpuprofile cpu.prof
go tool pprof -top -cum cpu.prof | head -12
```

O arráncalo con `-http` y obtén un perfil del proceso en marcha a través de `net/http/pprof`, como lo harías con un servidor en producción:

```bash
go run ./02-profiling/cmd/logreport -http localhost:6060 -duration 60s &
go tool pprof -http=: 'http://localhost:6060/debug/pprof/profile?seconds=5'
```

`pprof` guarda cada perfil que descarga en `$HOME/pprof/`, para que puedas volver a él más tarde.

## Puntos Clave

- Los profilers de Go **muestrean**: el de CPU a 100 Hz de tiempo de CPU por hilo (SIGPROF, temporizadores `timer_create` por hilo en Linux), el de heap aproximadamente una vez por cada 512 KB asignados
- El perfil de CPU muestra **tiempo de CPU, no esperas**. El total de muestras puede superar el tiempo real cuando hay varios hilos ocupados
- `alloc_space` encuentra el código que genera trabajo para el allocator y el GC; `inuse_space` encuentra lo que está reteniendo memoria
- `flat` muestra dónde se queman los ciclos; `cum` muestra quién es el responsable. Empieza con `-top -cum`, y sigue con `-peek` y `-list`
- Mide con un benchmark, explica con un perfil, arregla y **vuelve a perfilar**

## Para Ir Más Allá (opcional)

1. El siguiente punto caliente es `regexp.ReplaceAllString`. Sustituye la expresión regular por un bucle escrito a mano sobre los segmentos de la ruta. ¿Cuánto más rápido es? ¿Sigue siendo igual de fácil de leer?
2. Sustituye `strings.Fields` por una versión que no asigne un slice por línea (por ejemplo, `strings.Cut` tres veces). Comprueba la ganancia con `benchstat`.
3. Ejecuta `go test -bench . -memprofile mem.prof -memprofilerate 1` y compara los totales con los del perfil muestreado por defecto. ¿Cuánto se acercaba la estimación?
