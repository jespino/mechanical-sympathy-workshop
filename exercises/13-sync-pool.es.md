# sync.Pool

> **¿Quieres saber más?** Lee [`sync/pool.go`](https://github.com/golang/go/blob/go1.27.0/src/sync/pool.go) y la cola lock-free que tiene detrás en [`sync/poolqueue.go`](https://github.com/golang/go/blob/go1.27.0/src/sync/poolqueue.go). Después mira dos usuarios reales: `pp.free` en [`fmt/print.go`](https://github.com/golang/go/blob/go1.27.0/src/fmt/print.go) y `getBufferedEncoder`/`putBufferedEncoder` en [`encoding/json/jsontext/pools.go`](https://github.com/golang/go/blob/go1.27.0/src/encoding/json/jsontext/pools.go) (desde Go 1.27, `encoding/json` se apoya en la implementación v2, que guarda ahí sus encoders en pools).

En [Pre-asignación](03-preallocation.md) eliminamos asignaciones pidiendo el tamaño correcto una sola vez. Eso funciona cuando un objeto vive dentro de una única llamada. Pero mucho código caliente necesita un objeto *temporal* en cada llamada: un buffer para formatear una línea de log, el estado de un encoder, un slice auxiliar. Cada llamada lo asigna, lo usa durante un microsegundo y lo deja tirado para el GC. `sync.Pool` te permite devolver ese objeto y reutilizarlo en la siguiente llamada. En este módulo veremos cómo está construido el pool, por qué es tan rápido, por qué olvida cosas en cada GC y cómo usarlo sin pegarte un tiro en el pie.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás cómo `sync.Pool` guarda objetos por P, y por qué `Get`/`Put` casi nunca necesitan locks
- Entenderás la victim cache: por qué los objetos del pool sobreviven exactamente a un GC, y no más
- Sabrás por qué hay que guardar punteros en el pool y no valores (el SA6002 de staticcheck)
- Evitarás los errores clásicos: no resetear, quedarte con buffers enormes para siempre, mezclar tamaños
- Medirás el efecto del pool en allocs/op, en el número de ciclos de GC y en el rendimiento en paralelo

## El Concepto: Cómo Funciona `sync.Pool`

### Una pequeña caché por P

Recuerda la P del scheduler: el "procesador" que una goroutine necesita para ejecutar código Go, una por `GOMAXPROCS`. Un `Pool` mantiene un `poolLocal` por cada P (`sync/pool.go`):

```go
type Pool struct {
	noCopy noCopy

	local     unsafe.Pointer // local fixed-size per-P pool, actual type is [P]poolLocal
	localSize uintptr        // size of the local array

	victim     unsafe.Pointer // local from previous cycle
	victimSize uintptr        // size of victims array

	New func() any
}

type poolLocalInternal struct {
	private any       // Can be used only by the respective P.
	shared  poolChain // Local P can pushHead/popHead; any P can popTail.
}

type poolLocal struct {
	poolLocalInternal

	// Prevents false sharing on widespread platforms with
	// 128 mod (cache line size) = 0 .
	pad [128 - unsafe.Sizeof(poolLocalInternal{})%128]byte
}
```

![sync.Pool mantiene un slot privado y una cola compartida por cada P, y otros P pueden robar de las colas compartidas](images/13-pool.webp)

Fíjate en el relleno: el hueco de cada P ocupa sus propias líneas de caché, para que dos cores usando el pool nunca se peleen por la misma línea. Veremos exactamente por qué importa en [Contención y False Sharing](15-contention-false-sharing.md).

Este es `Put` (sin el código del race detector):

```go
l, _ := p.pin()
if l.private == nil {
	l.private = x
} else {
	l.shared.pushHead(x)
}
runtime_procUnpin()
```

`pin()` llama a `runtime_procPin`, que desactiva la expulsión (preemption) y devuelve el id de la P actual. Mientras está fijada, la goroutine no puede moverse a otra P, así que nadie más puede tocar `l.private`: guardar ahí **no necesita ni lock ni operación atómica**. Ese es el camino rápido. Si `private` está ocupado, el objeto va a `shared`, un `poolChain`: una lista enlazada de buffers circulares lock-free (`poolqueue.go`) en la que solo la P propietaria inserta y saca por la cabeza, y las demás P solo pueden sacar por la cola.

`Get` es el espejo: coge `private`; si no, saca de la cabeza de la cadena `shared` local; si no, `getSlow`. El camino lento **roba** de la cola de las cadenas de otras P, y solo si todas están vacías mira en la victim cache. Si todo eso falla, `Get` llama a `New`.

Así que en régimen estable, cuando cada goroutine hace `Get` y `Put` en la misma P, el pool cuesta un pin, una lectura y una escritura de un puntero. Ninguna contención entre cores.

### La victim cache: dos GCs para morir

Los objetos del pool no se guardan para siempre. Al principio de cada ciclo de GC, con el mundo parado, el runtime llama a `clearpools()` (`runtime/mgc.go`), que llama a `poolCleanup`:

```go
// Drop victim caches from all pools.
for _, p := range oldPools {
	p.victim = nil
	p.victimSize = 0
}

// Move primary cache to victim cache.
for _, p := range allPools {
	p.victim = p.local
	p.victimSize = p.localSize
	p.local = nil
	p.localSize = 0
}
```

![Un objeto en el pool sobrevive a un GC en la victim cache y se descarta en el segundo](images/13-victim.webp)

Un objeto que metes en el pool sobrevive al siguiente GC en la *victim cache*, y se descarta en el GC posterior. Antes de Go 1.13 no había victim cache y cada GC vaciaba todos los pools, lo que provocaba picos de asignaciones justo después de cada recolección. La demo llama a `runtime.GOMAXPROCS(1)`: en la victim cache, `getSlow` solo mira el hueco `private` de la P *actual*, así que si la goroutine se moviera a otra P durante `runtime.GC()` no encontraría el objeto. Ejecuta la demo:

```bash
cd labs
go run ./13-sync-pool/cmd/victim
```

```
Put, 0 x runtime.GC(), Get -> found=true
Put, 1 x runtime.GC(), Get -> found=true
Put, 2 x runtime.GC(), Get -> found=false
Put, 3 x runtime.GC(), Get -> found=false
```

Ese es el contrato: un `Pool` es una **caché**, no una lista de objetos libres. Nunca puedes contar con recuperar lo que metiste, y no deberías usarlo para objetos que son caros de *perder* (conexiones, ficheros abiertos). Es para objetos baratos de recrear pero molestos de asignar millones de veces por segundo.

### Guarda punteros, no valores

`Put` recibe un `any`. Convertir un valor en interfaz necesita un puntero a los datos, así que un valor que no es puntero se empaqueta (se copia al heap) en cada `Put`. Meter un `[]byte` (una cabecera de slice de 3 palabras) asigna 24 bytes cada vez, que es justo la asignación que intentabas evitar. Un `testing.AllocsPerRun` rápido en Go 1.27:

```
p.Put(b)   // b es un []byte    -> 1 asignación por Put/Get
p.Put(pb)  // pb es un *[]byte  -> 0 asignaciones
```

Staticcheck lo detecta como **SA6002**. Guarda `*bytes.Buffer`, `*[]byte`, `*miStruct`.

### Trampas

1. **Resetea lo que obtienes.** El objeto vuelve con lo que dejara en él el último usuario. `encoding/json` resetea cada encoder que saca de su pool (`e.s.reset(...)` en `getBufferedEncoder`). Si se te olvida, filtras datos entre peticiones.
2. **No te quedes con objetos enormes.** Si una petición de cada millón formatea un documento de 50 MB, su buffer crece hasta 50 MB y vuelve al pool. Ahora una P retiene 50 MB hasta que pasen dos GCs, y si siguen llegando peticiones grandes, para siempre. `fmt` se protege de esto en `pp.free`:

   ```go
   // Proper usage of a sync.Pool requires each entry to have approximately
   // the same memory cost. [...]
   // See https://golang.org/issue/23199.
   if cap(p.buf) > 64*1024 {
   	p.buf = nil
   } else {
   	p.buf = p.buf[:0]
   }
   ```

   `encoding/json` *no* tenía esa protección: hasta Go 1.26, `Marshal` siempre hacía `defer encodeStatePool.Put(e)`, fuera cual fuera el tamaño al que hubiera crecido el buffer. En Go 1.27 se apoya en la implementación v2, cuyo `putBufferedEncoder` siempre recicla los buffers de hasta 4 KB, y descarta uno más grande cuando se ha usado menos de un 25% cinco veces seguidas (ver https://go.dev/issue/27735).
3. **No mezcles tamaños.** Un pool de "buffers de cualquier tamaño" le da un buffer de 64 bytes a quien necesita 1 MB (que lo hará crecer, asignando de todas formas) y uno de 1 MB a quien necesita 64 bytes (desperdiciando memoria). Si los tamaños varían mucho, usa un pool por clase de tamaño (potencias de dos, por ejemplo).
4. **Nunca uses un objeto después de `Put`.** Puede que otra goroutine ya sea su dueña. Cosas como `return buf.Bytes()` después de `Put(buf)` son data races esperando a ocurrir.

## El Laboratorio

```
labs/13-sync-pool/
  before.go      WriteEvent ingenuo (línea base, no lo edites)
  after.go       tu copia: hazla rápida
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
  cmd/victim/    la demo de la victim cache de arriba
```

`WriteEventBefore(w, e)` formatea un evento de log estructurado como una línea JSON y la escribe en un `io.Writer`. Construye la línea en un `bytes.Buffer` nuevo en cada llamada. La línea tiene unos 260 bytes, así que el buffer crece varias veces por el camino. Hay dos benchmarks: `WriteEvent` lo llama desde una goroutine y `WriteEventParallel` lo llama desde `GOMAXPROCS` goroutines con `b.RunParallel`, como haría un servidor ocupado. Ambos añaden una columna extra, `GCs/Mop`: ciclos de GC por cada millón de llamadas.

### Paso 1: Mide la línea base

```bash
make bench LAB=13
```

Por ahora las dos columnas deberían ser iguales. Fíjate en las 7 asignaciones y ~1 KB por llamada.

### Paso 2: Encuentra las asignaciones

```bash
cd labs
go test -run '^$' -bench 'WriteEventParallel/impl=before' -memprofile mem.prof ./13-sync-pool/
go tool pprof -sample_index=alloc_space -top mem.prof
```

```
      flat  flat%   sum%        cum   cum%
 3859.91MB 83.22% 83.22%  3859.91MB 83.22%  bytes.growSlice
  479.02MB 10.33% 93.55%   479.02MB 10.33%  strconv.appendQuotedWith
  285.02MB  6.15% 99.70%  4144.93MB 89.37%  bytes.(*Buffer).grow
```

El crecimiento del buffer (64 → 128 → 256 → 512 bytes) es casi todo. El resto es `strconv.AppendQuote` haciendo crecer `AvailableBuffer()` cuando se queda pequeño para el valor. Un perfil de CPU (`-cpuprofile`) del mismo benchmark muestra `runtime.mallocgc` en torno a un tercio del tiempo (desde Go 1.27, las asignaciones pequeñas van a variantes especializadas por tamaño, así que también verás nombres como `runtime.mallocgcSmallNoScanSC6` debajo), más `gcBgMarkWorker` y `gcAssistAlloc` por las recolecciones que provoca toda esa basura.

### Paso 3: Arréglalo

Edita `labs/13-sync-pool/after.go`:

1. Declara un `sync.Pool` a nivel de paquete cuyo `New` devuelva `new(bytes.Buffer)`.
2. En `WriteEventAfter`: haz `Get` de un buffer, `Reset`, codifica y escribe.
3. Devuélvelo con `Put`, **salvo** que `buf.Cap()` supere los 64 KB.

¿Por qué es seguro reutilizar el buffer después de `w.Write(buf.Bytes())`? Porque el contrato de `io.Writer` dice: *"Write must not modify the slice data, even temporarily. Implementations must not retain p."* El writer ha terminado con nuestros bytes cuando `Write` retorna.

### Paso 4: Compara

```bash
make bench LAB=13
```

Resultados con la solución de referencia (`make solution LAB=13`) en un AMD Ryzen AI MAX+ 395 (16 cores, 32 hilos, compartido con otras cargas mientras se medía):

```
                      │    before    │                after                │
                      │    sec/op    │    sec/op     vs base               │
WriteEvent-32            879.1n ± 2%   563.4n ±  1%  -35.92% (p=0.000 n=8)
WriteEventParallel-32   274.60n ± 6%   48.19n ± 10%  -82.45% (p=0.000 n=8)

                      │   before    │                after                 │
                      │   GCs/Mop   │ GCs/Mop   vs base                    │
WriteEvent-32           342.8 ±  1%   0.0 ± 0%  -100.00% (p=0.000 n=8)
WriteEventParallel-32   256.6 ± 17%   0.0 ± 0%  -100.00% (p=0.000 n=8)

                      │    before    │                  after                   │
                      │     B/op     │     B/op      vs base                    │
WriteEvent-32           1.047Ki ± 0%   0.000Ki ± 0%  -100.00% (p=0.000 n=8)
WriteEventParallel-32   1.047Ki ± 0%   0.000Ki ± 0%  -100.00% (p=0.000 n=8)

                      │   before   │                 after                  │
                      │ allocs/op  │ allocs/op   vs base                    │
WriteEvent-32           7.000 ± 0%   0.000 ± 0%  -100.00% (p=0.000 n=8)
WriteEventParallel-32   7.000 ± 0%   0.000 ± 0%  -100.00% (p=0.000 n=8)
```

## Entendiendo los Resultados

**Cero asignaciones, cero GCs.** La versión before provoca un GC cada ~3.000 llamadas. La versión con pool reutiliza el mismo puñado de buffers todo el rato, así que el benchmark no produce basura y el GC nunca se ejecuta.

**Una goroutine: -36%.** Lo que queda es el formateo en sí. Lo que hemos quitado son siete llamadas a `mallocgc`, las copias de `growSlice` y el trabajo del GC (que, como en el módulo 03, se ejecuta sobre todo en *otros* cores y no aparece entero en el tiempo de esta goroutine).

**En paralelo: -82%.** Aquí es donde brilla el diseño por P. Con 32 goroutines asignando ~1 KB por llamada, el allocator y el GC son recursos compartidos: más ciclos de GC, mark assists robando tiempo a nuestras goroutines y más fallos de caché sobre memoria recién asignada. El camino rápido del pool solo toca el hueco `private` de la P actual, así que escala con los cores. Cuánto se gana en paralelo depende mucho de tu número de cores y de qué más se esté ejecutando. Prueba `-cpu 1,4,16` para ver cómo cambia.

> **Cuándo no usar un pool.** Si el objeto es pequeño y no escapa, el compilador ya lo pone en la pila ([Escape Analysis](05-escape-analysis.md)), y eso es más barato que cualquier pool. Si las asignaciones no aparecen en tus perfiles, un pool solo añade complejidad y una nueva clase de bugs (usar después de `Put`). Usa un pool para lo que el perfil te diga.

## Puntos Clave

- `sync.Pool` mantiene un hueco `private` y una cola `shared` lock-free **por P**: en régimen estable `Get`/`Put` no tocan memoria compartida
- Cuando la P local está vacía, `Get` roba de otras P, luego mira la victim cache y luego llama a `New`
- Cada GC mueve los pools a la victim cache y descarta las víctimas anteriores: los objetos sobreviven a un GC, no a dos
- Es una caché, no una lista de libres: nunca dependas de recuperar un objeto
- Guarda punteros (`*bytes.Buffer`, `*[]byte`), no valores: empaquetar un valor asigna en cada `Put`
- Resetea siempre, limita el tamaño de lo que devuelves (como hace `fmt` con 64 KB) y nunca toques un objeto después de `Put`

## Para Ir Más Allá (opcional)

1. Quita la comprobación de 64 KB de tu solución y añade un benchmark en el que 1 de cada 1.000 eventos tenga un `Message` de 1 MB. Usa `GODEBUG=gctrace=1` o `runtime.ReadMemStats` para ver cuánto heap sigue en uso.
2. Ejecuta `WriteEventParallel` con `-cpu 1,2,4,8,16`. ¿En qué se diferencian las curvas de before y after?
3. Sustituye el pool por un `chan *bytes.Buffer` de capacidad 64 usado como lista de libres. Haz un benchmark en paralelo. ¿Por qué es más lento que `sync.Pool`? (Pista: [Goroutines, Channels y el Scheduler](16-goroutines-channels.md).)
