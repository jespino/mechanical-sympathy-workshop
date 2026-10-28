# Proyecto Final: Optimiza un Analizador de Logs

> **¿Quieres saber más?** Ten a mano la documentación de [`runtime/pprof`](https://pkg.go.dev/runtime/pprof) y de [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat), y la página de [Diagnostics](https://go.dev/doc/diagnostics) de la documentación de Go. Todo lo demás que necesitas está en los módulos anteriores.

Este es el módulo en el que dejas de seguir pasos. Tienes un programa realista, un analizador de logs de acceso, escrito como se escribe mucho código de producción: correcto, legible y lleno de los anti-patrones que hemos estudiado. Tu trabajo es hacerlo rápido sin cambiar ni un solo byte de su salida. Nadie te dice qué arreglar. Te lo dice el profile.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Ejecutarás un ciclo de optimización completo: **profile → hipótesis → cambia una cosa → test → benchstat → repite**
- Reconocerás los anti-patrones del taller por su firma en el profile
- Priorizarás por coste medido, no por lo feo que parezca el código
- Llevarás un registro de cada paso, para que cada cambio esté respaldado por números
- Sabrás cuándo parar

## El Programa

`AnalyzeBefore(r io.Reader, w io.Writer)` lee líneas como esta:

```
2026-10-23T12:00:00.123Z GET /api/users/1234 200 1534us "Mozilla/5.0 (X11; Linux x86_64) ... Chrome/129.0.0.0 Safari/537.36"
```

(timestamp, método, ruta, status, latencia, user agent). Agrupa las peticiones por ruta, sustituyendo los segmentos numéricos de la ruta por `:id` y descartando las query strings, y escribe un informe:

```
lines: 100000  malformed: 187  routes: 64
window: 2026-10-23T12:00:00.015Z .. 2026-10-23T12:15:51.791Z

ROUTE                                   COUNT   P50(us)   P99(us)     2xx     3xx     4xx     5xx
GET /v2/reports/:id/export               3203      2747    418761    1705     372     766     360
GET /api/search                          3187      2733    451878    1699     375     758     355
GET /static/img/:id                      3183      2732    405572    1660     398     758     367
...
DELETE /api/products                      675      2866    584031     341      90     141     103

user agents:
  bot         12422  12.4%
  chrome      12706  12.7%
  ...
```

La entrada viene de `GenerateLog(n, seed)`, que es determinista e incluye alrededor de un 0,2% de líneas mal formadas (un status que falta, o una línea cortada). No se escribe nada en disco: los tests y los benchmarks generan sus datos en memoria.

Lee ahora `labs/19-capstone/before.go`, de principio a fin. Son ~200 líneas. Apunta lo que *tú* crees que es lento, y después deja que el profile te dé o te quite la razón.

## El Laboratorio

```
labs/19-capstone/
  gen.go               deterministic log generator (don't edit)
  before.go            the original analyzer (baseline, don't edit)
  after.go             your copy: make this one fast
  solution.go          reference solution (built with -tags solution)
  lab_test.go          output-equality tests + BenchmarkAnalyze
  cmd/loganalyzer/     run either version as a program, profile it, dump the data
```

Las reglas:

1. **El informe debe ser idéntico byte a byte.** `go test ./19-capstone/` compara `before` y `after` con tres semillas y con un conjunto de líneas escritas a mano con trampa (fechas inválidas, tabuladores, comillas dentro del user agent, `+7` como segmento de ruta, zonas horarias...). Tu parser debe aceptar y rechazar exactamente lo mismo que la regexp y `time.Parse`.
2. **Un cambio por iteración**, para que sepas qué te ha aportado cada cambio.
3. **Cada cambio lleva su benchstat.** Nada de "parece más rápido".

El benchmark analiza 100.000 líneas (~14 MB) por op y escribe el informe en `/dev/null` a través de un `*os.File`, como una CLI que escribe en stdout. El test usa 3.000 líneas, así que sigue siendo rápido incluso para `before.go`.

### El ciclo

Desde la raíz del repositorio:

```bash
make bench LAB=19                      # before vs after, saved to labs/19.bench.txt
cp labs/19.bench.txt labs/19-step0.bench.txt
```

Después, en cada iteración:

```bash
# 1. Profile your current version
cd labs
go test -run '^$' -bench 'Analyze/impl=after' -cpuprofile cpu.prof ./19-capstone/
go tool pprof -top cpu.prof | head -25        # where is the flat time?
go tool pprof -top -cum cpu.prof | head -40   # which of *your* functions own it?
go tool pprof -http=: cpu.prof                # flame graph, source view
cd ..

# 2. Hypothesize: "X costs ~N% because of Y. If I do Z, it should go away."
# 3. Change ONE thing in after.go
# 4. Check correctness
go test ./labs/19-capstone/        # or: make test-labs

# 5. Measure, and compare with the previous step
make bench LAB=19
cp labs/19.bench.txt labs/19-step1.bench.txt
benchstat -filter '/impl:after' labs/19-step0.bench.txt labs/19-step1.bench.txt
```

`make bench` siempre te muestra la distancia total desde `before`. La comparación con `-filter '/impl:after'` muestra lo que te ha aportado *este* paso. Usa también las columnas de `-benchmem` y `-memprofile` (`go tool pprof -sample_index=alloc_objects`) cuando sospeches de las asignaciones. Esos ficheros `*.bench.txt` están en el gitignore.

### Leyendo el profile: síntomas → módulos

| Lo que ves en el profile | Causa probable | Módulo |
|---|---|---|
| `runtime.mallocgc`, `newobject`, `growslice` altos en `-cum` | Asignaciones por elemento, slices que crecen desde vacío | [03](03-preallocation.md), [12](12-gc-pressure.md) |
| `gcBgMarkWorker`, `gcDrain`, `scanobject`, `markroot` | Trabajo del GC causado por la tasa de asignación, o punteros que escanear | [12](12-gc-pressure.md) |
| `stackScanState`, `scanstack`, `lfstack` | El GC escaneando **muchos stacks de goroutines** | [13](13-goroutines-channels.md), [12](12-gc-pressure.md) |
| `runtime.newproc`, `schedule`, `findRunnable`, `gopark` | Goroutines demasiado pequeñas para lo que cuestan | [13](13-goroutines-channels.md) |
| `sync.(*Mutex).Lock`, `lock2`, `futex`, `procyield` | Contención de locks | [16](16-contention-false-sharing.md) |
| `regexp.(*Regexp).doOnePass`, `doExecute`, `backtrack` | Regexp en el camino caliente | [04](04-strings-bytes.md) |
| `strings.genSplit`, `Join`, `ToLower`, `slicebytetostring`, `concatstrings` | Copias de strings y strings temporales por elemento | [04](04-strings-bytes.md) |
| `fmt.Sprintf`, `fmt.(*pp).doPrintf`, `convT64`, `convTstring` | fmt y boxing en interfaces | [04](04-strings-bytes.md), [08](08-interfaces-boxing.md) |
| `runtime.assertE2I`, `typeAssert`, `map[string]any` | Tipado dinámico en el camino caliente | [08](08-interfaces-boxing.md) |
| `sort.Slice`, `reflectlite.Swapper`, `sort.(*lessSwap)` | Ordenación basada en reflexión con closures | [08](08-interfaces-boxing.md) |
| `mapaccess*`, `mapassign*` | Demasiadas operaciones sobre maps, o maps donde bastaría un array | [03](03-preallocation.md), [10](10-struct-layout.md) |
| `time.Parse`, `strconv` | Trabajo de parsing que no hace falta para el resultado | este módulo |
| `syscall.Syscall`, `internal/poll.(*FD).Write` | I/O sin buffer | [14](14-io-syscalls.md) |
| Funciones pequeñas justo por encima del presupuesto de inlining en el bucle caliente | Coste de las llamadas, escapes que el inlining eliminaría | [06](06-inlining.md), [07](07-inlining-escape.md), [18](18-pgo.md) |

Fíjate también en la línea de cabecera de `go tool pprof -top`: `Duration: 1.05s, Total samples = 7.65s` significa que el programa mantuvo ocupados ~7,3 cores. El wall time es lo que mide `sec/op`, pero el tiempo de CPU es lo que mide tu factura del cloud.

### Paso 1: Línea base

Ejecuta `make bench LAB=19` y haz un profile de `impl=before`. Apunta las cinco primeras entradas de `-top` y de `-top -cum`. En un AMD Ryzen AI MAX+ 395, `before` tarda ~320ms por cada 100k líneas (~42 MB/s), reserva ~170 MB y hace 2,3 millones de asignaciones por ejecución.

### Paso 2 en adelante: tus iteraciones

Recorre el ciclo hasta que estés satisfecho. Un objetivo razonable es ser **10x** más rápido con **menos del 1% de las asignaciones**. La solución de referencia llega a ~17x.

Si te atascas, la sección **Pistas** del final va de lo vago a lo concreto. Intenta usarlas de una en una.

## Entendiendo los Resultados

> **Spoilers.** Lee esto después de tu propio intento.

Así se construyó la solución de referencia, paso a paso. Cada fila es el benchmark `impl=after` en ese paso (6 ejecuciones cada uno, AMD Ryzen AI MAX+ 395, con otros trabajos ejecutándose en la máquina). La columna "vs prev" es `benchstat` frente al paso anterior, todos con p=0.002.

| Paso | Cambio | sec/op | vs prev | B/op | allocs/op |
|---|---|---|---|---|---|
| 0 | `before.go` tal cual | 321.6ms | | 176.3 MiB | 2,355k |
| 1 | Sin goroutine por línea, sin mutex: un bucle normal | 240.9ms | -25% | 149.7 MiB | 2,069k |
| 2 | Parser escrito a mano que devuelve un struct, en lugar de regexp + `map[string]any` | 74.3ms | -69% | 89.7 MiB | 1,225k |
| 3 | Clave de ruta construida en un `[]byte` reutilizado, buscada con `m[string(key)]`. Familia del user agent cacheada por cada UA distinto | 26.7ms | -64% | 49.9 MiB | 1.9k |
| 4 | `slices.Sort` en lugar de `sort.Slice` | 24.0ms | -10% | 49.9 MiB | 1.7k |
| 5 | Clases de status en un `[10]int` en lugar de un `map[int]int` por ruta. Timestamps comparados como strings, sin `time.Parse` por línea | 22.0ms | -8% | 49.8 MiB | 1.4k |
| 6 | Lectura en streaming a través de un `bufio.Reader` y parsing de `[]byte` in situ (sin `ReadAll`, sin `Split`). Salida con buffer | 17.9ms | -19% | 2.5 MiB | 1.3k |

Y la comparación final (`make solution LAB=19`):

```
           │    before     │               after                │
           │    sec/op     │   sec/op     vs base               │
Analyze-32   322.24m ± 14%   18.48m ± 2%  -94.27% (p=0.002 n=6)

           │    before     │                 after                  │
           │      B/s      │      B/s       vs base                 │
Analyze-32   42.43Mi ± 12%   739.85Mi ± 2%  +1643.74% (p=0.002 n=6)

           │     before     │                after                │
           │      B/op      │     B/op      vs base               │
Analyze-32   167.722Mi ± 7%   2.489Mi ± 0%  -98.52% (p=0.002 n=6)

           │     before     │               after                │
           │   allocs/op    │  allocs/op   vs base               │
Analyze-32   2339.365k ± 1%   1.319k ± 0%  -99.94% (p=0.002 n=6)
```

**17,4x más rápido, 1.800x menos asignaciones, en un solo hilo.** Con un millón de líneas (143 MB), `go run ./19-capstone/cmd/loganalyzer -impl before -lines 1000000` tarda 3,1s, y la solución tarda 200ms.

Lo que enseña cada paso:

**Paso 1: la versión "paralela" era la parte más lenta.** El profile de la línea base mostraba 7,65s de CPU en 1,05s de wall time, y la entrada más grande no era la regexp. Era `runtime.(*lfstack).pop` bajo `runtime.getempty`, llamado desde `stackScanState.putPtr` y `stackScanState.addObject`: **el GC escaneando los stacks de hasta 100.000 goroutines vivas**, todas peleándose por una lista global de buffers de trabajo. El trabajo por línea es de unos pocos microsegundos como mucho, demasiado poco para compensar una goroutine más un mutex con contención. Pasar a secuencial solo fue un 25% más rápido en wall time, pero usaba **un core en lugar de ~7,3** (el profile del paso 1: `Total samples = 1.08s` en 1,02s). Si necesitas paralelismo, divide la entrada en unos pocos trozos grandes con una goroutine y un resultado privado por trozo, y combínalos al final (consulta Para Ir Más Allá).

**Paso 2: regexp y `map[string]any`.** `doOnePass` y `mallocgc` dominaban. Un match de regexp con submatches reserva memoria, y un `map[string]any` por línea hace boxing de cada valor y reserva un map. Un parser de ~40 líneas que comprueba la misma gramática eliminó dos tercios del tiempo. El test de casos límite es lo que hace que esto sea seguro: `\S` significa "ninguno de `\t\n\f\r `", y `\d{3}` significa exactamente tres dígitos ASCII.

**Paso 3: la mayor mejora en asignaciones.** `strings.Split` + `Join` en `normalizeRoute`, `fmt.Sprintf` para la clave y `strings.ToLower` sobre un user agent de 100 bytes reservaban memoria en cada línea. Construir la clave en un buffer reutilizado y buscarla con `m[string(buf)]` (el compilador no reserva memoria para esa conversión en un índice de map) llevó las asignaciones **de 1,2 millones a 1.857**. Solo hay 8 user agents distintos, así que clasificar cada uno una vez y cachear el resultado sale gratis.

**Pasos 4 y 5: los rendimientos decrecientes.** `sort.Slice` pasa por un swapper basado en reflexión y por un closure en cada comparación, mientras que `slices.Sort` es genérico y está especializado para `[]int`. Los timestamps de formato fijo se ordenan como strings, así que `time.Parse` solo se ejecuta dos veces por informe. Una comprobación de rango barata mantiene el comportamiento de "una fecha inválida es una línea mal formada". Estos cambios dieron ~10% y ~8%. Son reales, pero son el tipo de cambio que te saltarías si fueras justo de tiempo.

**Paso 6: la última copia.** `io.ReadAll` hace crecer un buffer hasta 14 MB (copiando sobre la marcha), después `string(data)` lo vuelve a copiar entero, y después `strings.Split` reserva un slice de 100k entradas. Leer en streaming a través de un `bufio.Reader` de 64 KiB y parsear `[]byte` in situ bajó B/op de 50 MB a 2,5 MB. Casi todo lo que queda son los propios slices de latencias.

**Y el que no importó: la salida sin buffer.** `strace -c` muestra que `before` hace 76 syscalls `write` (una por línea del informe), frente a 3 de la solución. A ~100ns cada una, son ~7µs de 300ms. La solución usa buffer en la salida porque no cuesta nada, pero **sigue usando `fmt.Fprintf` para el informe**. Se ejecuta ~75 veces por informe, así que es código frío, y optimizarlo solo lo haría más difícil de leer. El profile te dice dónde parar además de dónde empezar.

## Pistas

Úsalas de una en una, desde arriba.

1. Antes de cambiar nada, mira la *cabecera* del profile de la línea base (`Total samples`), y la parte de arriba de `-top` (flat), no solo `-cum`. ¿El programa pasa su tiempo en tu código o en el runtime?
2. ¿Quién llama a las funciones del runtime más calientes? Usa `go tool pprof -peek 'runtime.getempty$' cpu.prof`, o el flame graph.
3. ¿Cuánto tarda procesar una línea, comparado con arrancar una goroutine y coger un lock con contención? ¿Serían distintos 32 workers con maps privados, combinados al final, de 100.000 goroutines compartiendo un map?
4. Una vez que el bucle es secuencial, el parsing es el siguiente gran bloque. Una regexp con grupos de captura reserva memoria en cada match. ¿Cómo queda la gramática de la línea si la escribes como unos cuantos bucles `for`?
5. `map[string]any` hace boxing de cada valor (mira `convT64`, `convTstring`) y reserva un map por línea. Un struct pequeño devuelto por valor no reserva nada.
6. Ejecuta `-memprofile` con `-sample_index=alloc_objects`. ¿Qué tres funciones reservan memoria en cada línea? (`normalizeRoute`, `fmt.Sprintf`, `agentFamily`.)
7. `m[string(b)]` con `b []byte` no reserva memoria. Construye `"METHOD /route/:id"` en un `[]byte` que reutilices entre líneas.
8. ¿Cuántos user agents distintos hay en el log? ¿Necesitas pasar cada uno a minúsculas 100.000 veces?
9. `sort.Slice` frente a `slices.Sort`: mira `sort.(*lessSwap)` y `reflectlite.Swapper` en el profile.
10. Un `map[int]int` por ruta para los códigos de status, cuando solo necesitas `status/100`: ¿cuál es la estructura de datos más pequeña que funciona?
11. `"2026-10-23T12:00:00.015Z"` < `"2026-10-23T12:00:01.000Z"` como strings. ¿Cuándo se cumple eso para todos los timestamps? ¿Qué más tienes que comprobar para que las fechas inválidas sigan rechazándose?
12. `io.ReadAll` + `string(data)` + `strings.Split` copia la entrada entera dos veces y reserva un slice de 100k strings. `bufio.Reader.ReadSlice('\n')` te entrega cada línea in situ, como un `[]byte`. Cuidado con las líneas más largas que el buffer (`bufio.ErrBufferFull`).
13. La salida: `w` es un `*os.File` en el benchmark. ¿Tiene buffer? ¿Importa? Mide antes de decidir.

## Puntos Clave

- El ciclo es **profile → hipótesis → cambia una cosa → test → benchstat**. Guarda los números de cada paso
- Revisa la cabecera del profile: el tiempo de CPU puede ser muchas veces el wall time, y quemar cores también es un coste
- Repartir trabajo con la granularidad equivocada es peor que no repartirlo: las goroutines cuestan planificación, memoria y **escaneo de stacks por parte del GC**
- Las mayores mejoras vinieron de eliminar *asignaciones por elemento* (regexp, boxing, construcción de strings), no de micro-ajustes
- Un test de igualdad de la salida, con casos límite, es lo que te permite reescribir un parser sin miedo
- Para cuando el profile diga que lo que queda es código frío. Un `fmt` legible en un informe que se ejecuta una vez está bien

## Para Ir Más Allá (opcional)

1. **Paralelismo bien hecho.** Divide la entrada en `runtime.GOMAXPROCS(0)` trozos por límites de línea, dale a cada goroutine su propio `analyzer` y combina los resultados (concatena los slices de latencias, suma los contadores). ¿Cuánto se acerca la mejora a la lineal? ¿Cuánto cuesta la combinación? Mantén la salida idéntica.
2. **Percentiles sin ordenar.** Las latencias ocupan casi toda la memoria que queda. ¿Podría un histograma fijo (como los histogramas HDR) sustituir a los slices? ¿Por qué no puedes hacerlo aquí sin cambiar el informe?
3. **PGO encima.** Recoge un profile de tu versión final y recompila con `-pgo` (consulta [Optimización Guiada por Perfiles](18-pgo.md)). ¿Le queda algo por ganar al compilador?
4. **Ajuste del GC.** Ejecuta tu versión del paso 3 con `GOGC=400` y `GOGC=off`. ¿Cuánto del tiempo restante era GC? Después compáralo con la versión final. ¿Por qué apenas le afecta GOGC?
