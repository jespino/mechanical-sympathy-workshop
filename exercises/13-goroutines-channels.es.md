# Goroutines, Channels y el Scheduler

> **¿Quieres saber más?** Lee `newproc1`, `gfget` y `gfput` en [`runtime/proc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/proc.go), `startingStackSize` en [`runtime/stack.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/stack.go), y `chansend`/`chanrecv` en [`runtime/chan.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/chan.go). La [documentación de `go tool trace`](https://pkg.go.dev/cmd/trace) explica el visor de trazas.

Las goroutines y los channels son la característica más conocida de Go, y son baratos: mucho más baratos que los hilos del sistema operativo y los locks. Pero "barato" no es "gratis". Arrancar una goroutine cuesta cientos de nanosegundos, y una operación sobre un channel de decenas a cientos de nanosegundos, según la contención. Si el trabajo que le das a cada goroutine, o que envías en cada operación sobre un channel, son solo unos pocos nanosegundos, la coordinación cuesta más que el trabajo. En este módulo medimos esos costes, los vemos en una traza de ejecución y arreglamos dos patrones muy habituales **agrupando trabajo**: enviar bloques en lugar de elementos sueltos, y repartir el trabajo en rangos en lugar de lanzar una goroutine por elemento.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás qué hace `go f()` en el runtime: `newproc`, la lista de `g` libres por P, las pilas iniciales y `runnext`
- Sabrás qué hace un envío/recepción en un channel: el lock del channel, la entrega directa a un receptor en espera, `gopark`/`goready`
- Medirás el coste por elemento de un pipeline con channels y de una goroutine por elemento
- Capturarás y leerás una traza de ejecución de un benchmark con `go tool trace`
- Agruparás trabajo para que el coste de coordinación se reparta entre muchos elementos

## El Concepto: Lo Que Cuesta Coordinar

### Arrancar una goroutine

`go f()` se compila como una llamada a `runtime.newproc`, que ejecuta `newproc1` en la pila del sistema:

```go
mp := acquirem() // disable preemption because we hold M and P in local vars.
pp := mp.p.ptr()
newg := gfget(pp)
if newg == nil {
	newg = malg(stackMin)
	casgstatus(newg, _Gidle, _Gdead)
	allgadd(newg) // publishes with a g->status of Gdead so GC scanner doesn't look at uninitialized stack.
}
```

1. **Reutiliza una `g` muerta.** Cuando una goroutine termina, `gfput` mete su struct `g` (y, si tiene el tamaño estándar, su pila) en la lista `gFree` de la P. Cuando hay 64 o más, pasa un lote a una lista global. `gfget` recupera una. Así que la mayoría de sentencias `go` en un programa ocupado *no* asignan una `g` nueva.
2. **Si no, asigna una** con una pila de `stackMin = 2048` bytes, y la registra en `allgs`, la lista de todas las goroutines. Esa lista nunca se reduce.
3. **Prepara el marco** para que la goroutine empiece en `fn` y "retorne" a `goexit`.
4. **`runqput(pp, newg, true)`**: la nueva goroutine va al hueco `runnext` de la P, así que es la siguiente en ejecutarse en esta P, y `wakep()` puede despertar a una P ociosa para que robe trabajo.

**Las pilas empiezan en 2 KB, pero no siempre.** Desde Go 1.19, `GODEBUG=adaptivestackstart=1` es el valor por defecto (`runtime1.go`). En cada GC, `gcComputeStartingStackSize` fija `startingStackSize` al tamaño medio de las pilas escaneadas, así que si tus goroutines suelen crecer hasta 8 KB, las nuevas empiezan con 8 KB y se ahorran las copias. Las pilas demasiado pequeñas siguen creciendo copiándose (`morestack` → `newstack`), y las demasiado grandes las encoge el GC.

El laboratorio tiene una demo que arranca 100.000 goroutines que se bloquean en un channel:

```bash
cd labs
go run ./13-goroutines-channels/cmd/gocost
```

```
started 100000 goroutines in 86ms (861 ns each)
goroutine stacks:   2058 bytes per goroutine
total from OS:      2889 bytes per goroutine
```

Alrededor de **0,9 µs y 3 KB** por goroutine *nueva* en esta máquina: la pila de 2 KB, más el struct `g` y la contabilidad del runtime. Es minúsculo comparado con un hilo del sistema operativo (8 MB de pila reservada, microsegundos de syscall para crearlo), pero siguen siendo casi 1.000 nanosegundos. Una `g` reutilizada de la lista de libres es mucho más barata, como veremos en el laboratorio.

### Una operación sobre un channel

Un channel es un `hchan` (`runtime/chan.go`): un buffer circular, dos colas de espera de `sudog`s (una por cada emisor y receptor bloqueado) y un lock.

![Cada envío por un channel toma el lock del channel y puede despertar una goroutine; agrupar paga ese coste una vez por bloque en lugar de una vez por elemento](images/13-channel.webp)

```go
type hchan struct {
	qcount   uint           // total data in the queue
	dataqsiz uint           // size of the circular queue
	buf      unsafe.Pointer // points to an array of dataqsiz elements
	...
	recvq    waitq  // list of recv waiters
	sendq    waitq  // list of send waiters
	...
	lock mutex
}
```

`chansend` hace, en orden:

1. **`lock(&c.lock)`**. Cada operación sobre el channel, desde cualquier core, coge este lock. Un channel compartido por 32 goroutines es un lock disputado con una línea de caché rebotando entre cores (mediremos lo que cuesta en [Contención y False Sharing](16-contention-false-sharing.md)).
2. **Si hay un receptor esperando** (`c.recvq.dequeue()`), copia el valor **directamente en su pila** (`sendDirect`), saltándose el buffer, y le hace `goready`: lo mete en una cola de ejecución.
3. **Si no, y el buffer tiene sitio**, copia el valor en el buffer y retorna.
4. **Si no, se bloquea**: obtiene un `sudog`, lo encola en `sendq` y hace `gopark`. La goroutine queda fuera del scheduler hasta que llegue un receptor, que le hará `goready`.

Así que un envío sin buffer a un worker en espera es: lock, copia, unlock y despertar una goroutine (meterla en una cola de ejecución, quizá despertando una P y un hilo del sistema). Después hay que planificar al worker antes de que pueda procesar el elemento, y cuando termina vuelve a aparcarse en `chanrecv`. Cada paso es rápido, pero hay muchos pasos, y todos tocan estado compartido. Los channels con buffer se ahorran el aparcar/despertar cuando el buffer no está ni lleno ni vacío, pero siguen cogiendo el lock en cada operación.

### Verlo: `go tool trace`

El tracer de ejecución registra cada creación de goroutine, bloqueo, desbloqueo, syscall y GC, con marcas de tiempo. Puedes capturar una traza de cualquier benchmark:

```bash
go test -run '^$' -bench 'Sum/impl=before' -benchtime 20x -trace trace.out ./13-goroutines-channels/
go tool trace trace.out
```

El segundo comando abre una interfaz web (en un puerto aleatorio de localhost; para elegirlo usa `-http=:6060`, que desde Go 1.27 también escucha solo en localhost: escribe `-http=0.0.0.0:6060` para exponerlo). En **View trace by proc** verás cada P cambiando de goroutine cada pocos cientos de nanosegundos, como un código de barras, en lugar de barras largas y continuas de trabajo. Los mismos datos están disponibles como perfiles de pprof:

```bash
go tool trace -pprof=sync trace.out > sync.prof    # tiempo bloqueado en channels/mutex
go tool trace -pprof=sched trace.out > sched.prof  # tiempo listo para ejecutar pero esperando una P
go tool pprof -top sync.prof
```

## El Laboratorio

```
labs/13-goroutines-channels/
  before.go      SumBefore y MapBefore (línea base, no lo edites)
  after.go       tu copia: hazlas rápidas
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
  cmd/gocost/    la demo del coste de una goroutine de arriba
```

`work(x)` es un pequeño mezclador de enteros que tarda unos 11 ns por elemento: el tamaño de muchas tareas reales por elemento. Ambas funciones procesan 100.000 elementos con un paralelismo de `GOMAXPROCS`:

- **`SumBefore`** es un worker pool clásico: `GOMAXPROCS` workers leen elementos de un **`chan uint64` sin buffer**, un elemento por envío, y cada uno devuelve su suma parcial al final.
- **`MapBefore`** calcula `out[i] = work(items[i])` con **una goroutine por elemento** (`wg.Go`).

El benchmark `Sum` tiene además una columna `impl=sequential`: un bucle normal en una sola goroutine, la cifra a batir. Ambos benchmarks informan de `ns/item`.

### Paso 1: Mide la línea base

```bash
make bench LAB=13
```

Mira primero la columna `sequential`. Después mira `before`. Usar 32 hilos es **más lento** que usar uno.

### Paso 2: Mira la traza

```bash
cd labs
go test -run '^$' -bench 'Sum/impl=before' -benchtime 20x -trace trace.out ./13-goroutines-channels/
go tool trace -pprof=sync trace.out > sync.prof
go tool pprof -top sync.prof
```

```
      flat  flat%   sum%        cum   cum%
 6459.97ms 71.52% 71.52%  6459.97ms 71.52%  runtime.chanrecv2
 2407.41ms 26.65% 98.17%  2407.41ms 26.65%  runtime.chanrecv1
  164.97ms  1.83%   100%   164.97ms  1.83%  runtime.chansend1
         0     0%   100%  6459.97ms 71.52%  labs/13-goroutines-channels.SumBefore.func1
```

Los workers (`SumBefore.func1`) pasan la mayor parte del tiempo bloqueados en `chanrecv2` (la recepción de `for x := range jobs`) **esperando trabajo**. El único productor no consigue meter elementos lo bastante rápido: cada envío cuesta más que los ~11 ns de trabajo que reparte. Después abre `go tool trace trace.out` y mira las P: pequeñas astillas de trabajo separadas por planificación.

### Paso 3: Arréglalo

Edita `labs/13-goroutines-channels/after.go`:

1. **`SumAfter`**: haz que `jobs` sea un `chan []uint64` y envía bloques: `jobs <- items[i:min(i+1024, len(items))]`. Un sub-slice es solo una nueva cabecera de slice que apunta al mismo array. No se copia nada. Los workers recorren cada bloque. Un pequeño buffer (`GOMAXPROCS`) en `jobs` deja que el productor se adelante.
2. **`MapAfter`**: arranca `GOMAXPROCS` goroutines, no `len(items)`. Dale a cada una un bloque contiguo `[start, end)` de índices. Cada goroutine escribe su propia parte de `out`, así que la única sincronización necesaria es el `Wait` final.

### Paso 4: Compara

```bash
make bench LAB=13
```

Resultados con la solución de referencia (`make solution LAB=13`) en un AMD Ryzen AI MAX+ 395 (16 cores, 32 hilos, compartido con otras cargas mientras se medía):

```
        │     before     │               after                │              sequential               │
        │    sec/item    │  sec/item    vs base               │   sec/item    vs base                 │
Sum-32    147.450n ± 13%   2.143n ± 3%  -98.55% (p=0.000 n=8)   11.645n ± 3%  -92.10% (p=0.000 n=8)
Map-32    283.250n ±  7%   2.320n ± 9%  -99.18% (p=0.000 n=8)

        │     before      │                after                │
        │      B/op       │     B/op      vs base               │
Map-32    7815.3Ki ±   0%   787.3Ki ± 0%  -89.93% (p=0.000 n=8)

        │    before     │               after               │
        │   allocs/op   │ allocs/op   vs base               │
Map-32    200002.00 ± 0%   66.00 ± 0%  -99.97% (p=0.000 n=8)
```

(`benchstat` muestra nuestra métrica `ns/item` como `sec/item`. La tabla de `sec/op` son los mismos datos multiplicados por 100.000, y los B/op y allocs/op de `Sum` son el ruido de unas pocas goroutines, así que los omitimos.)

## Entendiendo los Resultados

**El pipeline con channels era 13 veces más lento que un bucle normal.** 147 ns por elemento con 32 hilos frente a 11,6 ns con uno. Cada elemento pagaba un lock sobre un channel compartido, una copia y normalmente una pareja aparcar/despertar, todo serializado a través de un único productor y un único lock. Añadir workers no ayudaba, porque el cuello de botella era el channel, no el trabajo.

**Enviar 1.024 elementos por operación lo hizo 69 veces más rápido**: 2,1 ns por elemento, **5,4 veces más rápido que el secuencial**. El coste del channel es el mismo por operación, pero ahora se divide entre 1.024. Cada worker pasa el tiempo en un bucle cerrado sobre memoria contigua, que es lo que mejor se les da a las CPUs.

**Una goroutine por elemento costaba 280 ns por elemento y 2 asignaciones**: el closure que se pasa a `wg.Go`, y el closure que el propio `wg.Go` pasa a `go`. Son unos 80 bytes por elemento, ~8 MB de basura por llamada para el GC. Es más barato que los 0,9 µs de `cmd/gocost` porque las goroutines que terminan vuelven a la lista `gFree` y se reutilizan enseguida, pero sigue siendo 25 veces el coste del trabajo. Con una goroutine por P, cada una con su rango, son 2,3 ns por elemento y 66 asignaciones por llamada (los 787 KB que quedan son el propio slice `out`).

¿Por qué no es 32 veces más rápido que el secuencial con 32 hilos? Con 100.000 elementos × 11 ns, todo el trabajo es solo ~1,1 ms, así que arrancar los workers y esperar al más lento pesa. Además solo hay 16 cores físicos (los hilos SMT comparten unidades de ejecución), y había otras cargas ejecutándose en la máquina. Con más cores verás más diferencia entre before y after, porque las versiones before *empeoran* con más contención.

> **Regla práctica:** haz que cada unidad de coordinación (una goroutine, una operación sobre un channel, la adquisición de un lock) lleve al menos **microsegundos** de trabajo. Si una tarea son nanosegundos, agrupa tareas hasta que deje de serlo. Y compara siempre con la versión secuencial: un paralelismo más lento que un solo core es más habitual de lo que crees.

### Cuándo una goroutine por cosa *sí* es lo correcto

Una goroutine por conexión o por petición es idiomático, y correcto: cada una hace milisegundos de trabajo, sobre todo esperando I/O, y el runtime la aparca en el netpoller gratis mientras espera. Agrupar importa cuando el trabajo por goroutine o por mensaje es pequeño y de CPU. Para paralelismo acotado sobre una entrada grande, el patrón es un número fijo de workers sobre bloques (o `errgroup` con `SetLimit`). Y cuando las goroutines se acumulan bloqueadas para siempre, el **perfil de fugas de goroutines** de Go 1.27 (`goroutineleak` en `runtime/pprof`, `/debug/pprof/goroutineleak` en `net/http/pprof`) muestra las que el GC puede demostrar que nunca se despertarán.

## Puntos Clave

- `go f()` → `newproc`: reutiliza una `g` muerta de la lista de libres de la P cuando puede, y mete la nueva goroutine en `runnext`
- Las goroutines nuevas empiezan con una pila de 2 KB, que se adapta en cada GC al tamaño medio de pila (`adaptivestackstart`, activado por defecto). Aquí una goroutine nueva bloqueada costó ~0,9 µs y ~3 KB
- Cada operación sobre un channel coge el lock del channel. Enviar a un receptor en espera copia directamente a su pila y le hace `goready`, y bloquearse significa `gopark`
- Los envíos por elemento y una goroutine por elemento cuestan 100–300 ns por elemento, así que con tareas pequeñas son más lentos que un bucle normal
- Agrupa: envía bloques `[]T` (los sub-slices no cuestan nada), dale a cada worker un rango contiguo
- `go test -trace` + `go tool trace` (y `-pprof=sync|sched`) muestran dónde se bloquean y esperan las goroutines

## Para Ir Más Allá (opcional)

1. Cambia `chunkSize` a 16, 64, 256, 4096 y 65536. Dibuja `ns/item`. ¿Dónde deja de mejorar, y por qué un bloque muy grande vuelve a empeorar?
2. Haz que `jobs` en `SumBefore` tenga buffer (`make(chan uint64, 1024)`) en una copia. ¿Cuánto ayuda? ¿Por qué no arregla el problema?
3. Ejecuta el benchmark `Map` con `-cpu 1,4,16`. ¿Cómo escala una goroutine por elemento?
4. Ejecuta `cmd/gocost` con `GODEBUG=adaptivestackstart=0` después de hacer que las goroutines usen una función recursiva profunda antes de bloquearse. ¿Qué cambia?
