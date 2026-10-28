# Ayudando al Garbage Collector

> **¿Quieres saber más?** Lee la [guía oficial del GC de Go](https://go.dev/doc/gc-guide), después el pacer en [`runtime/mgcpacer.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mgcpacer.go), el código de marcado en [`runtime/mgcmark.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mgcmark.go) y el nuevo marcador Green Tea en [`runtime/mgcmark_greenteagc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mgcmark_greenteagc.go). El formato de `gctrace` está documentado en [`runtime/extern.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/extern.go).

Hasta ahora hemos reducido *asignaciones*: menos objetos significa menos ciclos de GC. Este módulo mira la otra mitad de la factura. **¿Cuánto cuesta cada ciclo de GC?** La respuesta sorprende a mucha gente: apenas depende de cuánta basura generas. Depende de cuánta memoria *viva* tienes y, sobre todo, de **cuántos punteros hay en ella**. Un programa con una caché grande y llena de punteros paga por esa caché en cada ciclo de GC, para siempre. Lo vamos a medir, haremos la caché invisible para el GC y después aprenderemos los dos mandos que controlan cada cuánto se ejecuta el GC: `GOGC` y `GOMEMLIMIT`.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás cómo funciona el GC concurrente mark-sweep de Go, y qué cambia Green Tea (el GC por defecto desde Go 1.26)
- Entenderás el modelo de coste: el trabajo de marcado es proporcional al heap vivo *con punteros*
- Sabrás por qué los objetos `noscan` (sin punteros dentro) son casi gratis para el GC, y que los strings también son punteros
- Leerás una línea de `GODEBUG=gctrace=1` campo a campo
- Sabrás cómo calcula el pacer el objetivo de heap, y cómo cambiar memoria por CPU con `GOGC` y `GOMEMLIMIT`

## El Concepto: Lo Que Hace Realmente el GC

### Marcado y barrido concurrentes

El GC de Go es un recolector **mark-sweep tricolor, concurrente y que no mueve objetos**. Cada ciclo tiene cuatro fases:

1. **Terminación del barrido** (stop-the-world, normalmente decenas de microsegundos): termina el barrido del ciclo anterior y activa la write barrier.
2. **Marcado concurrente**: partiendo de las *raíces* (pilas de las goroutines, variables globales), sigue cada puntero y marca cada objeto alcanzable. Se ejecuta a la vez que tu código, en workers de fondo dedicados (el 25% de `GOMAXPROCS`, `gcBackgroundUtilization = 0.25`), en P ociosas y mediante **mark assists**: a las goroutines que asignan demasiado deprisa se les obliga a hacer parte del marcado.
3. **Terminación del marcado** (stop-the-world, corta).
4. **Barrido concurrente**: los objetos sin marcar se liberan, de forma perezosa, a medida que se reutilizan los spans.

Liberar basura es barato: barrer es solo mirar bits de marca. Lo caro es **marcar**, y el marcado solo toca objetos *vivos*. La basura nunca se visita.

### El modelo de coste: lo que cuesta son los punteros

Cuando el marcador llega a un objeto, tiene que escanearlo: leer cada palabra que podría ser un puntero y seguirla. Pero Go conoce el tipo de cada asignación, y el allocator coloca los objetos **sin ningún puntero** en spans *noscan* separados. Mira `greyobject` en `runtime/mgcmark.go`:

```go
// If this is a noscan object, fast-track it to black
// instead of greying it.
if span.spanclass.noscan() {
	gcw.bytesMarked += uint64(span.elemsize)
	return
}
```

![El GC sigue todos los punteros del heap; la memoria sin punteros nunca se escanea](images/12-mark.webp)

Un objeto noscan se marca y ya está: el GC nunca lee su contenido. Un `[]byte` o `[]float64` de 100 MB cuesta lo mismo de marcar que uno de 16 bytes. Un `[]*T` de 100 MB son 13 millones de punteros que seguir, cada uno hacia un objeto distinto que hay que encontrar, marcar y escanear a su vez.

Así que el coste de un ciclo de GC es, aproximadamente:

> **trabajo de marcado ∝ (heap vivo que contiene punteros) + (pilas) + (globales)**

Cosas que contienen punteros y que la gente suele olvidar: `string` (puntero + longitud), slices, maps de tipos con punteros, interfaces, `time.Time` (tiene un `*Location`).

### Green Tea: el GC por defecto desde Go 1.26

Go 1.25 incluyó un nuevo algoritmo de marcado como `GOEXPERIMENT=greenteagc`. Desde Go 1.26 está **activado por defecto**, y lo sigue estando en Go 1.27 (`GreenTeaGC: true` en la configuración base de `internal/buildcfg/exp.go`; aún puedes desactivarlo con `GOEXPERIMENT=nogreenteagc` al compilar). El comentario al principio de `mgcmark_greenteagc.go` explica la idea:

> *"achieve better locality during mark/scan by delaying scanning so that we can accumulate objects to scan within the same span, then scan the objects that have accumulated on the span all together."*

El marcador clásico mantenía una pila LIFO de objetos individuales y saltaba por todo el heap siguiendo punteros, con un fallo de caché por objeto. Green Tea encola **spans** enteros (FIFO), de modo que cuando llega el momento de escanear un span, ya se han marcado varios de sus objetos, y se escanean juntos desde las mismas pocas líneas de caché. En CPUs con AVX-512 (como el Zen 5 que usamos abajo) incluso escanea spans de objetos pequeños con SIMD (`internal/runtime/gc/scan`).

Green Tea hace que seguir cada puntero sea más barato. No cambia el modelo de coste: lo que cuesta siguen siendo los punteros. Mediremos ambas cosas.

### El pacer: ¿cuándo empieza un ciclo?

El pacer decide el **objetivo de heap**, el tamaño de heap al que el siguiente ciclo debería haber terminado. En `gcControllerState.commit` (`mgcpacer.go`):

```go
gcPercentHeapGoal = c.heapMarked + (c.heapMarked+c.lastStackScan.Load()+c.globalsScan.Load())*uint64(gcPercent)/100
```

![El objetivo del heap es el heap vivo más un GOGC por ciento; un ciclo de GC empieza cuando el heap se acerca al objetivo](images/12-pacer.webp)

Con el valor por defecto `GOGC=100`: si sobrevivieron 200 MB al último ciclo, el siguiente debería terminar cuando el heap llegue a ~400 MB. También hay un mínimo, `heapMinimum` (4 MB con `GOGC=100`, `defaultHeapMinimum`), para que los heaps diminutos no estén recolectando constantemente. Así que:

- **GOGC** cambia memoria por CPU. `GOGC=200` deja crecer el heap hasta 3 veces el heap vivo y ejecuta la mitad de ciclos. `GOGC=50` hace lo contrario. `GOGC=off` desactiva el objetivo proporcional. También puedes llamar a `debug.SetGCPercent(n)` en tiempo de ejecución.
- **GOMEMLIMIT** (Go 1.19+, `debug.SetMemoryLimit`) es un **límite blando a la memoria total que usa el runtime**. Cuando te acercas a él, el pacer usa un objetivo de heap más bajo (`memoryLimitHeapGoal`), diga lo que diga `GOGC`. Para evitar una espiral de muerte cuando el heap vivo está cerca del límite, un limitador de CPU deja el GC en un máximo de aproximadamente el 50% de la CPU (`mgclimit.go`). El patrón habitual en contenedores es `GOGC=off GOMEMLIMIT=<~90% del contenedor>`: nada de GC hasta que haga falta.

### Leyendo `gctrace`

`GODEBUG=gctrace=1` imprime una línea por ciclo en stderr. Esta es una del laboratorio de este módulo:

```
gc 10 @0.537s 1%: 0.041+15+0.011 ms clock, 1.3+0/117/307+0.36 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
```

| Campo | Significado |
|---|---|
| `gc 10` | décimo ciclo de GC desde que arrancó el programa |
| `@0.537s` | tiempo desde que arrancó el programa |
| `1%` | parte de la CPU dedicada al GC desde el arranque |
| `0.041+15+0.011 ms clock` | tiempo real de: terminación del barrido (STW) + marcado concurrente + terminación del marcado (STW) |
| `1.3+0/117/307+0.36 ms cpu` | tiempo de CPU de: terminación del barrido + marcado dividido en **assist / background / idle** + terminación del marcado |
| `286->286->286 MB` | heap al empezar el ciclo -> al terminar -> heap vivo (marcado) |
| `573 MB goal` | objetivo de heap para el siguiente ciclo: 286 × 2, porque `GOGC=100` |
| `0 MB stacks, 0 MB globals` | pilas y globales escaneables, las otras raíces |
| `32 P` | `GOMAXPROCS` |
| `(forced)` | lanzado por `runtime.GC()`, no por el pacer |

Las pausas (el primer y el tercer número) son minúsculas. La fase de marcado tardó 15 ms de tiempo real pero **424 ms de CPU** repartidos entre cores. Esa CPU es lo que pagas, en cada ciclo, por este heap.

## El Laboratorio

```
labs/12-gc-pressure/
  before.go      StoreBefore: []*Record + map[int64]*Record (línea base, no lo edites)
  after.go       tu copia: hazla sin punteros
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
  cmd/gctune/    experimento con GOGC / GOMEMLIMIT
```

`StoreBefore` es una tabla en memoria de registros de usuarios, indexada por ID: Go perfectamente idiomático.

```go
type Record struct {
	ID    int64
	Name  string
	Email string
	Score float64
	Age   int32
}

type StoreBefore struct {
	records []*Record
	byID    map[int64]*Record
}
```

Con 2 millones de registros son 6 millones de objetos en el heap (2M `Record` y 4M arrays de strings) y unos **8 millones de punteros** que el GC tiene que seguir en cada ciclo: dos strings por registro, más 2M en el slice y otros 2M en el map. Hay tres benchmarks, cada uno con un store de 2M registros:

- `GC`: el coste de un `runtime.GC()` completo con el store vivo. Coste de marcado puro.
- `Churn`: un "manejador de peticiones" junto al store que asigna 16 KB de basura de vida corta por operación. Informa de `gc-cpu-ns/op`, el tiempo de CPU del GC por operación (de `runtime/metrics`, `/cpu/classes/gc/total:cpu-seconds`).
- `AverageScore`: un recorrido completo de los registros, para comprobar que el nuevo layout no es más lento para el trabajo normal.

### Paso 1: Mide la línea base

```bash
make bench LAB=12
```

Este tarda uno o dos minutos: construir un store de 2M registros lleva su tiempo.

### Paso 2: Mira el GC

```bash
cd labs
go test -c -o gc.test ./12-gc-pressure/
GODEBUG=gctrace=1 ./gc.test -test.run '^$' -test.bench 'GC/impl=before' -test.benchtime 3x 2>&1 | grep '^gc ' | tail -3
```

```
gc 10 @0.537s 1%: 0.041+15+0.011 ms clock, 1.3+0/117/307+0.36 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
gc 11 @0.560s 2%: 0.062+13+0.011 ms clock, 2.0+0/105/260+0.35 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
gc 12 @0.584s 2%: 0.033+13+0.010 ms clock, 1.0+0/104/216+0.33 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
```

286 MB vivos, ~14 ms de marcado en tiempo real y ~370 ms de CPU de marcado, en cada ciclo. Nada cambia en el store entre ciclos, pero el GC no puede saberlo: vuelve a seguir los 8 millones de punteros cada vez.

### Paso 3: Elimina los punteros del store

Edita `labs/12-gc-pressure/after.go`. Haz los tres cambios de uno en uno, en este orden, y ejecuta `make bench LAB=12 BENCH=GC` después de cada uno, para ver lo que aporta cada cambio por sí solo:

1. **Los strings son punteros.** Guarda todos los nombres y emails en un único `text []byte` grande, y deja en el registro solo offsets `uint32` y longitudes `uint16`. `Lookup` reconstruye el string con `string(s.text[off:off+len])`.
2. **Guarda los registros por valor**: `records []recordAfter`, no `[]*recordAfter`. Ahora el slice es una única asignación grande y, como `recordAfter` no tiene punteros, acaba en un span noscan.
3. **Quita los punteros del map**: `map[int64]int32`, un índice dentro de `records`. Como ni las claves ni los valores contienen punteros, sus grupos también se asignan como noscan.

Ahora todo el store son tres objetos: uno con punteros (el propio struct `StoreAfter`) y dos arrays noscan grandes, más los grupos noscan del map. Ejecuta `go test ./12-gc-pressure/` para comprobar que `Lookup` y `AverageScore` siguen coincidiendo.

### Paso 4: Compara

```bash
make bench LAB=12
```

Resultados con la solución de referencia (`make solution LAB=12`) en un AMD Ryzen AI MAX+ 395 (16 cores, 32 hilos, compartido con otras cargas mientras se medía):

```
                │    before     │                after                │
                │    sec/op     │    sec/op     vs base               │
GC-32             22769.9µ ± 3%   387.4µ ± 19%  -98.30% (p=0.002 n=6)
Churn-32           1367.5n ± 6%   819.4n ±  8%  -40.08% (p=0.002 n=6)
AverageScore-32     7.627m ± 4%   1.409m ±  4%  -81.52% (p=0.002 n=6)

         │    before     │                after                 │
         │ gc-cpu-sec/op │ gc-cpu-sec/op  vs base               │
Churn-32   13512.0n ± 9%    137.2n ± 11%  -98.98% (p=0.002 n=6)
```

(`benchstat` renombra nuestra unidad `gc-cpu-ns/op` a `gc-cpu-sec/op`. Las tablas de B/op y allocs/op son idénticas para ambos, así que las omitimos.)

Y el `gctrace` del nuevo store:

```
gc 8 @0.357s 0%: 0.038+0.14+0.007 ms clock, 1.2+0/0.38/0.062+0.24 ms cpu, 210->210->210 MB, 420 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
```

## Entendiendo los Resultados

**Un GC completo pasó de 23 ms a 0,39 ms, 59 veces más rápido**, y el heap sigue ocupando 210 MB. El heap solo es un 27% más pequeño, así que no es su tamaño lo que ha cambiado. El *número de punteros* ha pasado de ~8 millones a casi cero. El GC marca los tres objetos grandes, ve `noscan` y se detiene.

**Churn: la CPU del GC por operación es ~98 veces menor**, y el propio manejador de peticiones es un 40% más rápido. En el caso before, los workers de marcado de fondo y los assists gastaban unos 13,5 µs de CPU por cada operación de 1,4 µs. Es 10 veces más CPU en el GC que en el trabajo real. No aparece entero en `sec/op` porque la mayor parte se ejecuta en cores que de otro modo estarían ociosos. En un servidor de producción ocupado, esos cores no están ociosos.

**El recorrido también es 5,4 veces más rápido.** `[]recordAfter` es un único array contiguo: el prefetcher hardware lo lee en streaming (más sobre esto en [Cachés de CPU y Localidad de Datos](15-cpu-caches.md)). `[]*Record` obliga a la CPU a cargar un puntero y luego saltar al registro. Los layouts sin punteros ayudan tanto al GC como a tus propios bucles.

**¿Qué aporta Green Tea?** Puedes compilar el mismo benchmark con el marcador antiguo y comparar (misma máquina):

```bash
GOEXPERIMENT=nogreenteagc go test -c -tags solution -o old.test ./12-gc-pressure/
```

```
                  │ nogreenteagc │         greenteagc (default)         │
                  │    sec/op    │    sec/op     vs base                │
GC/impl=before-32   33.90m ± 32%   22.37m ±  2%  -34.01% (p=0.000 n=10)
GC/impl=after-32    649.0µ ±  6%   419.5µ ± 14%  -35.37% (p=0.000 n=10)
```

Green Tea recorta en torno a un tercio el tiempo de marcado del store con punteros, gratis (con Go 1.26 medimos en torno a una cuarta parte; el marcador antiguo es muy ruidoso aquí, mira su ±32%). Cambiar el layout de los datos lo recortó un **98%**. Un GC mejor ayuda, pero el mejor trabajo del GC es el que no tiene que hacer.

> **Compromisos.** Los offsets dentro de un `[]byte` no son gratis: no puedes borrar un registro sin dejar un hueco, `Lookup` ahora asigna un string (quien lo llame podría usar los bytes directamente) y el código es menos obvio. Hazlo para estructuras grandes y de vida larga (cachés, índices, tablas en memoria) que un perfil o `gctrace` muestren dominando la fase de marcado, no para todo.

## Ajustando GOGC y GOMEMLIMIT

`cmd/gctune` mantiene vivo un `StoreBefore` de 1M registros (~140 MB vivos) y asigna 8 GiB de buffers de 16 KB de vida corta a su lado. Después informa de los ciclos de GC, el tiempo de CPU del GC, el tiempo real y el RSS máximo. Ejecútalo con distintas configuraciones:

```bash
cd labs
GOGC=50 go run ./12-gc-pressure/cmd/gctune
GOGC=off GOMEMLIMIT=400MiB go run ./12-gc-pressure/cmd/gctune
go run -tags solution ./12-gc-pressure/cmd/gctune -store=after
```

```
store=before GOGC=25    GOMEMLIMIT=off       GCs= 203  GC CPU= 24.98s  wall= 2.21s  peak RSS= 219 MiB
store=before GOGC=50    GOMEMLIMIT=off       GCs=  96  GC CPU= 11.71s  wall= 1.15s  peak RSS= 279 MiB
store=before GOGC=100   GOMEMLIMIT=off       GCs=  45  GC CPU=  5.18s  wall= 0.70s  peak RSS= 418 MiB
store=before GOGC=200   GOMEMLIMIT=off       GCs=  24  GC CPU=  3.08s  wall= 0.51s  peak RSS= 615 MiB
store=before GOGC=400   GOMEMLIMIT=off       GCs=  12  GC CPU=  1.44s  wall= 0.50s  peak RSS= 951 MiB
store=before GOGC=100   GOMEMLIMIT=300MiB    GCs=  85  GC CPU= 10.28s  wall= 1.10s  peak RSS= 293 MiB
store=before GOGC=off   GOMEMLIMIT=400MiB    GCs=  50  GC CPU=  6.07s  wall= 0.75s  peak RSS= 386 MiB
store=before GOGC=off   GOMEMLIMIT=1GiB      GCs=  12  GC CPU=  1.52s  wall= 0.55s  peak RSS= 984 MiB
store=after  GOGC=100   GOMEMLIMIT=off       GCs=  78  GC CPU=  0.11s  wall= 0.39s  peak RSS= 230 MiB
store=after  GOGC=25    GOMEMLIMIT=off       GCs= 314  GC CPU=  0.36s  wall= 0.38s  peak RSS= 138 MiB
```

Léelo como una curva de compromiso:

- **GOGC es un dial entre memoria y CPU.** Cada vez que duplicas GOGC, el número de ciclos y la CPU del GC se reducen más o menos a la mitad, y sube el pico de memoria. El coste de cada ciclo sigue siendo el mismo (~120 ms de CPU, el coste de marcar el store).
- **GOMEMLIMIT limita la memoria en lugar de fijar una proporción.** `GOGC=off GOMEMLIMIT=1GiB` se comporta aquí como `GOGC=400`, porque es ahí donde cae el límite. Con un límite ajustado (`300MiB`, cerca del heap vivo más la basura en vuelo), el GC se ejecuta el doble de veces que con la configuración por defecto. Si pones el límite demasiado cerca del heap vivo, el programa pasa la mayor parte del tiempo recolectando. Para eso está el limitador del 50% de CPU.
- **El layout gana a los mandos.** Con el store sin punteros, `GOGC=25` ejecuta 314 ciclos (más que ninguna otra fila, porque el heap vivo es más pequeño) y aun así usa **en torno a 1/14 de la CPU de GC** de la configuración por defecto con el store lleno de punteros, con un tercio de la memoria. Ajustar GOGC te mueve a lo largo de la curva. Quitar punteros te da una curva mejor.

La columna "GC CPU" es mucho mayor que el tiempo real porque el marcado se ejecuta en paralelo en hasta 32 cores, usando P ociosas. En una máquina sin cores libres, esa CPU sale de tu procesamiento de peticiones. Los números dependen mucho del número de cores.

## Puntos Clave

- Marcar cuesta ∝ heap vivo **con punteros** + pilas + globales. La basura y la memoria sin punteros (`noscan`) son casi gratis
- Strings, slices, maps, interfaces y `time.Time` contienen punteros
- Estructuras grandes y de vida larga: mejor `[]T` que `[]*T`, índices en lugar de punteros, un `[]byte` + offsets en lugar de muchos strings, y claves y valores de map sin punteros
- Green Tea (por defecto desde 1.26) escanea span a span para mejorar la localidad, y aquí abarata el marcado ~33%. Cambiar el layout lo abarató un 98%
- Objetivo de heap = vivo + (vivo + pilas + globales) × GOGC/100. `GOGC` cambia memoria por CPU, `GOMEMLIMIT` limita la memoria
- `GODEBUG=gctrace=1` te dice cada cuánto se ejecuta el GC, cuánto ocupa el heap vivo y cuánta CPU se lleva el marcado

## Para Ir Más Allá (opcional)

1. Añade un campo `time.Time` a `Record` en una copia del store after. Vuelve a ejecutar el benchmark `GC`. ¿Qué ha pasado, y por qué?
2. Ejecuta `cmd/gctune` con `GOGC=off GOMEMLIMIT=200MiB`. Fíjate en el tiempo de CPU del GC y en el tiempo real. ¿Qué está haciendo el runtime?
3. Usa `go tool pprof` sobre un perfil de CPU de `BenchmarkChurn/impl=before`. ¿Cuánto tiempo se va a `gcBgMarkWorker` y cuánto a `gcAssistAlloc`?
4. Compara la métrica `/gc/scan/heap:bytes` de `runtime/metrics` para ambos stores. ¿Qué relación tiene con el tiempo de marcado?
