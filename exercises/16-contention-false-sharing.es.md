# Contención y False Sharing

> **¿Quieres saber más?** Lee el mutex en [`internal/sync/mutex.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/sync/mutex.go) (desde Go 1.24, `sync.Mutex` es un envoltorio fino sobre él), los semáforos en los que se aparca en [`runtime/sema.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/sema.go) y las constantes de relleno en [`internal/cpu/cpu.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/cpu/cpu.go). La guía de diagnóstico explica los [perfiles de mutex y de bloqueo](https://go.dev/doc/diagnostics#profiling).

Hasta ahora casi todos nuestros benchmarks se ejecutaban en un solo core. Los servidores reales se ejecutan en todos, y ahí aparece un nuevo tipo de coste: cores **esperándose unos a otros**. A veces es evidente, como un mutex que necesitan todas las goroutines. Otras veces es invisible en el código: dos goroutines escribiendo dos variables *distintas* que resultan vivir en la misma línea de caché de 64 bytes, y esa línea rebotando entre cores millones de veces por segundo. En este módulo cogemos un contador compartido, la estructura de datos concurrente más sencilla que existe, y hacemos que escale de 1 a 32 hilos.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás cómo funciona `sync.Mutex`: camino rápido, spinning, aparcado, modo normal y modo starvation
- Entenderás por qué una suma atómica sobre una variable compartida tampoco escala (coherencia de caché)
- Sabrás qué es el false sharing, cómo reconocerlo en un benchmark y cómo arreglarlo con relleno
- Repartirás (sharding) un contador caliente para que cada core escriba en su propia línea de caché
- Encontrarás contención con el perfil de mutex y el perfil de bloqueo

## El Concepto: Cores Esperándose Unos a Otros

### Dentro de `sync.Mutex`

Desde Go 1.24, `sync.Mutex` simplemente envuelve a `internal/sync.Mutex`. Todo su estado es un `int32` más un semáforo:

```go
const (
	mutexLocked = 1 << iota // mutex is locked
	mutexWoken
	mutexStarving
	mutexWaiterShift = iota
	...
	starvationThresholdNs = 1e6
)
```

Los bits altos cuentan los que esperan. `Lock` hace:

1. **Camino rápido**: un `CompareAndSwap` de 0 a `mutexLocked`. Sin contención, eso es todo lo que cuesta un lock (~8 ns en nuestro benchmark, dos operaciones atómicas por cada par Lock/Unlock).
2. **Spinning**: si el mutex está cogido, `lockSlow` puede girar en vacío unas pocas veces (`runtime_canSpin`: como mucho `active_spin = 4` iteraciones de 30 instrucciones `PAUSE`, y solo en una máquina multicore con otras P ejecutando y la cola local vacía), con la esperanza de que el dueño lo suelte pronto.
3. **Aparcado**: si no, incrementa el contador de espera y llama a `runtime_SemacquireMutex`. La goroutine se duerme en un semáforo de la `semTable` del runtime (251 cubos, cada uno un treap de goroutines en espera protegido por un lock del runtime) y la P ejecuta otra cosa. `Unlock` con goroutines esperando llama a `runtime_Semrelease` para despertar a una.

El comentario de `mutex.go` describe los dos **modos de equidad**:

> *"In normal mode waiters are queued in FIFO order, but a woken up waiter does not own the mutex and competes with new arriving goroutines over the ownership. [...] If a waiter fails to acquire the mutex for more than 1ms, it switches mutex to the starvation mode. In starvation mode ownership of the mutex is directly handed off from the unlocking goroutine to the waiter at the front of the queue."*

El modo normal es más rápido (una goroutine que está ejecutando puede volver a coger el lock sin cambio de contexto), y el modo starvation acota la latencia de cola. En cualquier caso, con mucha contención, las goroutines se pasan el tiempo aparcándose y despertándose en lugar de trabajar.

### Atómicos y coherencia de caché

"Usa un atómico y ya está" elimina el aparcado. Pero mira lo que hace el hardware con `atomic.AddInt64` (un `LOCK XADD` en amd64). Cada core tiene sus propias cachés L1/L2, y se mantienen coherentes con un protocolo de la familia **MESI** (Modified, Exclusive, Shared, Invalid; AMD usa MOESI, una variante). Para *escribir* una línea de caché, un core tiene que tenerla en estado Modified/Exclusive, lo que significa **invalidar la copia de todos los demás cores**. Así que cuando 32 cores hacen `Add(1)` sobre la misma variable:

- el core A obtiene la línea, suma, y entonces el core B la pide
- la línea viaja (de decenas a más de cien ciclos entre cores, más entre CCDs) hasta B, que suma, y luego la pide C...

Las operaciones quedan serializadas por la línea de caché moviéndose de un lado a otro. Eso es el **ping-pong de líneas de caché**. Un atómico es más rápido que un mutex, pero sobre una variable compartida sigue siendo una cola que atiende de uno en uno.

### False sharing

El arreglo parece evidente: dale a cada goroutine su propio contador y súmalos al leer. Pero la coherencia funciona por **líneas de caché** (64 bytes en amd64; `getconf LEVEL1_DCACHE_LINESIZE` imprime 64 en la máquina usada aquí), no por variables. Si tus shards son un `[]atomic.Int64`, ocho de ellos comparten una línea. Goroutines distintas escriben variables distintas, pero el hardware ve escrituras a la misma línea y la hace rebotar igualmente. Eso es **false sharing**: lo compartido solo existe en el hardware.

![Dos núcleos que escriben contadores distintos en la misma línea de caché se la pasan de uno a otro; el padding da a cada contador su propia línea](images/16-false-sharing.webp)

El arreglo es el **relleno** (padding): haz que cada shard ocupe su propia línea.

```go
type paddedShard struct {
	n atomic.Int64
	_ [64 - 8]byte
}
```

La librería estándar de Go hace exactamente esto. `internal/cpu` define `CacheLinePad` con `CacheLinePadSize = 64` en amd64 (128 en arm64 y ppc64, 256 en s390x), exportado para todo el mundo como `golang.org/x/sys/cpu.CacheLinePad`. Y ya viste en [sync.Pool](11-sync-pool.md) que `sync.Pool` rellena sus huecos por P hasta 128 bytes. Algunas CPUs de Intel precargan parejas de líneas adyacentes, así que 128 bytes es una elección conservadora habitual. En esta CPU Zen 5 no medimos ninguna diferencia entre 64 y 128 bytes.

## El Laboratorio

```
labs/16-contention-false-sharing/
  before.go      interfaz Counter + CounterBefore (sync.Mutex) (línea base, no lo edites)
  steps.go       AtomicCounter y ShardedCounter: los pasos intermedios (no lo edites)
  after.go       tu CounterAfter: haz que escale
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmark
```

El contador es la típica métrica de "peticiones servidas": `Inc` se llama todo el rato desde todas las goroutines, `Load` muy de vez en cuando (una lectura de métricas). `Inc(worker int)` recibe el id de quien llama, para que una implementación repartida pueda elegir un shard (las versiones con mutex y con atómico lo ignoran). `BenchmarkInc` usa `b.RunParallel` y ejecuta cuatro implementaciones:

| `impl=` | Qué es |
|---|---|
| `before` | un `int64` detrás de un `sync.Mutex` |
| `atomic` | un `atomic.Int64` |
| `sharded` | un `atomic.Int64` por shard, contiguos (`[]atomic.Int64`) |
| `after` | tu versión |

El número de shards es `GOMAXPROCS` redondeado hacia arriba a una potencia de dos.

### Paso 1: Mide la línea base con varios números de cores

La contención va de *cómo escala*, así que ejecuta los benchmarks con varios valores de `GOMAXPROCS` usando `CPU=` (se pasa a `go test -cpu`):

```bash
make bench LAB=16 CPU=1,2,4,8,16,32
```

Con `RunParallel`, `ns/op` es el tiempo real dividido entre el número *total* de incrementos de todas las goroutines. Un contador que escale perfectamente reduce su ns/op a la mitad cada vez que duplicas `-cpu`.

### Paso 2: Encuentra la contención

```bash
cd labs
go test -run '^$' -bench 'Inc/impl=before' -mutexprofile mutex.out ./16-contention-false-sharing/
go tool pprof -top -cum mutex.out
```

```
      flat  flat%   sum%        cum   cum%
         0     0%     0%     34.73s 83.94%  labs/16-contention-false-sharing.(*CounterBefore).Inc
    23.88s 57.70% 57.70%     31.70s 76.61%  sync.(*Mutex).Unlock (inline)
    17.50s 42.29%   100%     17.50s 42.29%  runtime.unlock (inline)
         0     0%   100%      7.82s 18.91%  internal/sync.runtime_Semrelease
```

El perfil de mutex mide **retraso**: 41 segundos de goroutines esperando en un benchmark que duró alrededor de un segundo, porque había hasta 32 goroutines esperando a la vez. El retraso se apunta al `Unlock` que terminó la espera, es decir, al código que *tiene* el lock. Fíjate en `runtime.unlock`: hay tanta contención que hasta el lock de la propia tabla de semáforos del runtime (en `sema.go`) está disputado. `go test` activa el perfil por ti. En un programa real, llama a `runtime.SetMutexProfileFraction(n)` (muestrea 1 de cada n eventos) y lee `/debug/pprof/mutex`. El **perfil de bloqueo** relacionado (`-blockprofile`, `runtime.SetBlockProfileRate`) cubre además operaciones sobre channels, `select`, `sync.Cond` y `WaitGroup.Wait`.

Las versiones atómica y repartida no aparecen en este perfil: el ping-pong de líneas de caché no es un lock, y ningún perfil lo muestra directamente. Lo encuentras como haremos ahora mismo: un benchmark que no escala con `-cpu`. O con contadores hardware (`perf c2c` en Linux se creó para encontrar false sharing).

### Paso 3: Arréglalo

Edita `labs/16-contention-false-sharing/after.go`:

1. Sustituye el mutex y el `int64` por `shards []atomic.Int64` y `mask int`, dimensionados con `numShards()` (copia `ShardedCounter` de `steps.go`). `Inc` hace `c.shards[worker&c.mask].Add(1)` y `Load` suma los shards.
2. Haz el benchmark. Apenas mejora a `atomic`. ¿Por qué?
3. Rellena cada shard hasta 64 bytes (`struct { n atomic.Int64; _ [56]byte }`) y vuelve a medir.

### Paso 4: Compara

```bash
make bench LAB=16 CPU=1,2,4,8,16,32      # tu versión
make solution LAB=16 CPU=1,2,4,8,16,32   # la referencia
```

En un AMD Ryzen AI MAX+ 395 (16 cores, 32 hilos, compartido con otras cargas mientras se medía):

```
        │     before     │                atomic                 │               sharded                │                after                │
        │     sec/op     │     sec/op      vs base               │    sec/op      vs base               │    sec/op     vs base               │
Inc         8.551n ±  1%     4.228n ±  1%  -50.56% (p=0.000 n=8)     4.385n ± 1%  -48.72% (p=0.000 n=8)    4.445n ± 1%  -48.01% (p=0.000 n=8)
Inc-2      11.840n ±  3%     6.692n ±  3%  -43.48% (p=0.000 n=8)     7.001n ± 8%  -40.87% (p=0.000 n=8)    2.213n ± 2%  -81.31% (p=0.000 n=8)
Inc-4      26.880n ±  3%     7.476n ± 12%  -72.19% (p=0.000 n=8)     7.118n ± 7%  -73.52% (p=0.000 n=8)    1.126n ± 1%  -95.81% (p=0.000 n=8)
Inc-8     47.0250n ± 13%    9.2840n ±  6%  -80.26% (p=0.000 n=8)   10.6300n ± 5%  -77.40% (p=0.000 n=8)   0.5766n ± 3%  -98.77% (p=0.000 n=8)
Inc-16    48.3200n ±  6%   11.1900n ±  7%  -76.84% (p=0.000 n=8)    6.1540n ± 7%  -87.26% (p=0.000 n=8)   0.3080n ± 2%  -99.36% (p=0.000 n=8)
Inc-32    51.1450n ±  3%   14.2600n ±  2%  -72.12% (p=0.000 n=8)    3.0060n ± 2%  -94.12% (p=0.000 n=8)   0.2083n ± 6%  -99.59% (p=0.000 n=8)
```

## Entendiendo los Resultados

Lee cada columna de arriba abajo: así es como escala.

**El mutex (`before`) se vuelve más lento al añadir cores**: 8,6 ns en solitario, 47 ns con 8 hilos y 51 ns con 32. Añadir cores empeora el rendimiento *total*, porque cada vez se va más tiempo en spinning, aparcar y despertar.

**El atómico** es 2 veces más rápido con un solo hilo (una operación atómica en lugar de dos, sin llamadas a funciones), pero también se vuelve *más lento* con más cores: de 4 ns a 14 ns. Cada incremento espera a que la línea de caché viaje hasta su core.

**El repartido sin relleno es el interesante.** Hasta 8 hilos es igual que el atómico, o un poco peor. Con 8 hilos hay 8 shards × 8 bytes = **exactamente una línea de caché**: para el hardware *es* una única variable compartida. Con 16 hilos hay 2 líneas y con 32 hay 4, así que empieza a mejorar, pero solo porque hay menos cores compartiendo cada línea. Eso es false sharing.

**Repartido y con relleno (`after`) escala perfectamente**: 4,4 → 2,2 → 1,1 → 0,58 → 0,31 → 0,21 ns. Cada vez que se duplican los hilos, el tiempo por incremento se reduce a la mitad, hasta los 16 cores físicos (el último paso a 32 es SMT, dos hilos por core, y gana menos). Cada core incrementa una línea que se queda en su propia L1. Con 32 hilos es **250 veces más rápido que el mutex y 70 veces más rápido que el atómico**.

Tus números dependen del número de cores, de la topología de la CPU (los cores en CCDs distintos están más lejos) y de lo que más se esté ejecutando en la máquina, pero la forma debería ser la misma.

> **Compromisos.** El contador con relleno usa 64 bytes por shard en lugar de 8, y `Load` ahora es O(shards) y no es una foto atómica de todos los shards en el mismo instante. Para métricas está bien. Para algo como "reserva una de N entradas", no. La lección general: cuando algo está caliente y compartido, busca la forma de que *no* esté compartido (por core, por goroutine, por petición) y combina de vez en cuando.

### Dónde se esconde el false sharing en código real

- Arrays de structs por worker: `stats [N]struct{ hits, misses int64 }` que actualiza cada worker
- Un mutex al lado de los datos calientes que protege, o dos mutex calientes juntos en un struct
- Un campo que casi solo se lee (un puntero de configuración) al lado de un contador que se escribe mucho: los lectores no paran de perder la línea
- El layout de los structs también importa aquí ([Layout de Structs y Clases de Tamaño](10-struct-layout.md)): los campos `_ [56]byte` son la única vez que *quieres* desperdiciar espacio

## Puntos Clave

- `sync.Mutex`: camino rápido con CAS → spinning breve → aparcado en un semáforo del runtime. El modo starvation se activa tras esperar 1 ms
- Con contención, un mutex se vuelve *más lento* al añadir cores
- Los atómicos evitan el aparcado, pero un único atómico compartido sigue serializándose en las transferencias de la línea de caché
- La coherencia funciona por líneas de 64 bytes (en amd64): variables distintas en la misma línea también rebotan. Eso es false sharing
- Reparte las escrituras calientes por worker/core, rellena los shards hasta una línea de caché (`cpu.CacheLinePad`, o 64/128 bytes) y combina al leer
- Encuentra la contención de locks con los perfiles de mutex/bloqueo. Encuentra la contención de líneas de caché con benchmarks de escalado con `-cpu` (y `perf c2c`)

## Para Ir Más Allá (opcional)

1. Cambia `paddedShard` a 128 bytes. ¿Hay alguna diferencia en tu CPU?
2. Usa un `int64` normal en cada shard en lugar de `atomic.Int64` (cada worker es dueño de su shard). Ejecuta `go test -race`. ¿Por qué se queja el race detector, si solo una goroutine escribe en cada shard?
3. Ejecuta `perf c2c record` y `perf c2c report` sobre el benchmark `sharded` (Linux). ¿Ves la línea de caché compartida?
4. Sustituye el mutex de `CounterBefore` por un `sync.RWMutex` y usa `RLock` en `Load`. ¿`Inc` es más rápido? ¿Por qué no?
