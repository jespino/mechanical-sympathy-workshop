# Cachés de CPU y Localidad de Datos

> **¿Quieres saber más?** [What Every Programmer Should Know About Memory](https://people.freebsd.org/~lstewart/articles/cpumemory.pdf), de Ulrich Drepper, es el clásico (largo, pero las secciones 3 y 6 son lo esencial). La charla de Scott Meyers [CPU Caches and Why You Care](https://www.youtube.com/watch?v=WDIkqP4JbkE) es la versión de una hora. En Go, las propias estructuras de datos del runtime están llenas de trucos pensados para la caché: busca `cpu.CacheLinePad` en [`internal/cpu/cpu.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/cpu/cpu.go).

Este es el primer módulo de la Parte IV, donde dejamos Go atrás y miramos el hardware que hay debajo. En [Layout de Structs y Clases de Tamaño](10-struct-layout.md), un struct se hizo un 43% más pequeño y un bucle que lo recorría pasó a ser un 45% más rápido sin cambiar ni una línea del bucle. Eso es porque el bucle no estaba limitado por la CPU: estaba esperando a la memoria. En este módulo miramos la maquinaria que hay entre la CPU y la RAM, la jerarquía de cachés, y tres patrones de acceso que trabajan a su favor o en su contra.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Conocerás los niveles de la jerarquía de memoria y sus latencias, medidas en tu propia máquina
- Entenderás las cache lines, la localidad espacial y la localidad temporal
- Sabrás qué hace el prefetcher hardware, y por qué perseguir punteros lo anula
- Arreglarás tres patrones clásicos hostiles a la caché: recorrido por columnas, `[]*T` y array de structs con un solo campo caliente
- Usarás `perf stat` para contar fallos de caché (Linux)

## El Concepto: La Jerarquía de Memoria

Un core moderno puede ejecutar varias instrucciones por nanosegundo. La DRAM necesita unos 100 ns para responder a una petición de una dirección que no ha visto recientemente. Las cachés cubren esa distancia: memorias pequeñas y rápidas que guardan copias de los datos usados recientemente cerca del core.

![La jerarquía de memoria: cada nivel es más grande y más lento que el anterior](images/15-hierarchy.webp)

Puedes consultar los tamaños de caché de tu máquina con `lscpu` o desde sysfs:

```bash
lscpu | grep -i cache
grep . /sys/devices/system/cpu/cpu0/cache/index*/{level,type,size,coherency_line_size}
```

En el AMD Ryzen AI MAX+ 395 usado para este taller (16 cores, 32 threads):

| Nivel | Tamaño | Compartida por | Tamaño de línea |
|---|---|---|---|
| L1 datos | 48 KB | 1 core | 64 B |
| L2 | 1 MB | 1 core | 64 B |
| L3 | 32 MB | 8 cores (un CCD; hay dos CCD en total) | 64 B |
| DRAM | 64 GB | todos | |

En macOS, `sysctl hw.l1dcachesize hw.l2cachesize hw.cachelinesize` te da la misma información.

### Midiendo la latencia

El tamaño es solo la mitad de la historia. El laboratorio incluye un programa que mide la **latencia** de una carga para conjuntos de datos (working sets) de tamaño creciente. Sigue un ciclo aleatorio de punteros, donde la dirección de cada carga depende de la carga anterior, así que la CPU no puede solapar ni predecir nada:

```bash
cd labs
go run ./15-cpu-caches/cmd/latency
```

```
working set      16 KB:    1.0 ns per load
working set      32 KB:    1.0 ns per load
working set      64 KB:    3.0 ns per load
working set     256 KB:    3.0 ns per load
working set     512 KB:    3.4 ns per load
working set    1024 KB:    5.5 ns per load
working set    2048 KB:   10.8 ns per load
working set    8192 KB:   11.2 ns per load
working set   16384 KB:   13.5 ns per load
working set   32768 KB:   80.5 ns per load
working set   65536 KB:  134.8 ns per load
working set  262144 KB:  157.0 ns per load
```

La jerarquía se lee directamente en los números: ~1 ns mientras todo cabe en L1 (48 KB), ~3 ns en L2, ~10-14 ns en L3, y después un precipicio hasta **80-160 ns** en cuanto el working set supera los 32 MB de L3. (En los tamaños grandes, parte del coste son también fallos de TLB: la caché de la CPU para traducciones de direcciones virtuales a físicas también se queda corta.) Un acceso a DRAM cuesta lo mismo que **150 aciertos en L1** o más.

### Cache lines

Las cachés no mueven bytes, mueven **cache lines**: 64 bytes alineados en todos los x86 actuales y en la mayoría de cores ARM (la serie M de Apple usa líneas de 128 bytes). Lee un `int64` y pagarás por la línea entera en la que está. Dos consecuencias:

- **Localidad espacial**: si tu siguiente acceso está en la misma línea, es gratis. Leer un `[]int64` secuencialmente cuesta un fallo cada 8 elementos.
- **Localidad temporal**: si vuelves a tocar los mismos datos pronto, siguen en la caché. Si tocas muchos otros datos entremedias, han sido expulsados.

![Recorrer por filas usa cada byte de cada línea de caché; recorrer por columnas toca una línea nueva en cada acceso](images/15-traversal.webp)

### El prefetcher hardware

La CPU observa tus fallos. Cuando ve un patrón, como líneas consecutivas o un salto (stride) constante, empieza a traer líneas **antes de que las pidas**, así que un recorrido secuencial de un slice va cerca del **ancho de banda** de la memoria (decenas de GB/s) en lugar de pagar la **latencia** completa en cada línea.

Lo que el prefetcher no puede hacer es adivinar el destino de un puntero que todavía no ha cargado. En `for _, p := range ptrs { sum += p.X }`, la dirección del siguiente `p.X` está en memoria, así que el prefetcher no puede verla venir. Eso es **perseguir punteros** (pointer chasing), y es justo lo que hace a propósito el programa de latencia de arriba.

> **¿Y qué pinta Go aquí?** El compilador de Go no reordena tus bucles ni tus estructuras de datos para mejorar la localidad, y nunca convierte un `[]*T` en un `[]T`. Eso es cosa tuya. La buena noticia es que Go te da tipos valor, slices contiguos y control sobre el layout de los structs, que es todo lo que necesitas.

## El Laboratorio

```
labs/15-cpu-caches/
  before.go      implementaciones hostiles a la caché (línea base, no editar)
  after.go       tu copia: hazla amigable con la caché
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
  cmd/latency/   el programa de latencia de arriba
```

Todos los conjuntos de datos son más grandes que la L3 a propósito, para que los efectos se vean:

| Benchmark | Datos | Problema |
|---|---|---|
| `ColumnSums` | matriz de 4096×4096 `float64`, por filas (128 MB) | la recorre columna a columna: cada acceso está a 32 KB del anterior |
| `PtrLedger` | 1M × `*Account` (64 MB de cuentas), barajadas | cada acceso sigue un puntero a una cache line aleatoria |
| `Ledger` | 1M × `Account` (64 MB), array de structs | necesita 8 bytes de cada cuenta de 64 bytes, carga los 64 |

### Paso 1: Mide la línea base

```bash
make bench LAB=15
```

### Paso 2: Cuenta los fallos (opcional, solo Linux)

> **perf solo existe en Linux.** En macOS y Windows, sáltate este paso: los benchmarks por sí solos cuentan la historia. En macOS, la plantilla *CPU Counters* de Instruments puede mostrar contadores hardware parecidos si quieres profundizar.

`perf stat` lee los contadores hardware de rendimiento de la CPU mientras se ejecuta tu programa. Muchas distribuciones (Ubuntu y Debian incluidas) vienen con `kernel.perf_event_paranoid` a 4, lo que bloquea a los usuarios sin privilegios. Si `perf stat true` dice *"No supported events found"* o *"Access to performance monitoring … is limited"*, bájalo hasta el próximo reinicio:

```bash
sudo sysctl kernel.perf_event_paranoid=1
```

Compila el binario de test una vez, y después ejecuta un benchmark cada vez bajo `perf stat`. Usa un número fijo de iteraciones (`-test.benchtime 20x`) para que las dos ejecuciones hagan la misma cantidad de trabajo:

```bash
cd labs
go test -c -o caches.test ./15-cpu-caches/
perf stat -e cache-references,cache-misses,L1-dcache-loads,L1-dcache-load-misses \
    ./caches.test -test.run '^$' -test.bench 'ColumnSums/impl=before' -test.benchtime 20x
perf stat -e cache-references,cache-misses,L1-dcache-loads,L1-dcache-load-misses \
    ./caches.test -test.run '^$' -test.bench 'ColumnSums/impl=after' -test.benchtime 20x
```

*(Ejecuta ambos y compara. Apunta aquí tus números.)*

Qué buscar:

- `L1-dcache-loads` debería ser parecido en las dos ejecuciones: el bucle hace el mismo número de cargas.
- `L1-dcache-load-misses` y `cache-misses` deberían caer en picado con `impl=after`. El orden por columnas falla en casi cada carga; el orden por filas falla más o menos una vez cada 8 `float64`, y el prefetcher oculta la mayoría de esos fallos.
- Los contadores incluyen la preparación del test (construir una matriz de 128 MB), que es igual en las dos ejecuciones. Sube `-test.benchtime` si tapa la diferencia.

Los nombres de los eventos dependen de la CPU. `cache-references`/`cache-misses` son alias genéricos (en AMD se mapean a eventos de L2/L3, no exactamente a la "last-level cache"). Ejecuta `perf list cache` para ver cuáles soporta la tuya.

### Paso 3: Arréglalo

Edita `labs/15-cpu-caches/after.go`:

1. **`ColumnSumsAfter`**: intercambia los bucles. Recorre fila a fila y acumula en `sums[j]`. El slice `sums` ocupa solo 32 KB, así que se queda en L1 durante toda la ejecución.
2. **`PtrLedgerAfter`**: guarda `[]Account` en lugar de `[]*Account`, y rellénalo directamente en `NewPtrLedgerAfter`.
3. **`LedgerAfter`**: convierte el array de structs en un **struct de arrays**: un slice por campo (`ids`, `balances`, `created`, …), y haz que `Total` recorra solo `balances`.

`go test ./15-cpu-caches/` comprueba que los resultados son los mismos. (En `ColumnSums`, cada columna se sigue sumando en el mismo orden, así que los resultados en coma flotante son idénticos bit a bit.)

### Paso 4: Compara

```bash
make bench LAB=15
```

Solución de referencia (`make solution LAB=15`) en un AMD Ryzen AI MAX+ 395:

```
              │    before    │               after                │
              │    sec/op    │   sec/op     vs base               │
ColumnSums-32   77.889m ± 5%   8.286m ± 6%  -89.36% (p=0.002 n=6)
PtrLedger-32     3.614m ± 5%   1.386m ± 4%  -61.63% (p=0.002 n=6)
Ledger-32       1392.0µ ± 3%   289.0µ ± 5%  -79.24% (p=0.002 n=6)
```

Fíjate en las columnas B/op y allocs/op: ahí no ha cambiado nada. Todas estas mejoras son puramente de patrón de acceso a memoria.

## Entendiendo los Resultados

**ColumnSums: 9 veces más rápido** con el mismo número de sumas. El orden por columnas lee un `float64` por cada línea de 64 bytes y avanza 32 KB cada vez, cruzando un límite de página de 4 KB en cada acceso. Para cuando llega la siguiente columna, esas líneas hace tiempo que salieron de la caché, así que pagamos cada línea de la matriz de 128 MB **8 veces**. El orden por filas usa los 8 valores de cada línea, y el prefetcher trae la matriz a todo el ancho de banda.

**PtrLedger: 2,6 veces más rápido.** Mismas cuentas, mismo bucle, mismo resultado. Con los punteros en orden aleatorio, cada `a.Balance` es una carga dependiente a una línea impredecible, y el prefetcher no puede ayudar. La ejecución fuera de orden todavía solapa varias de estas cargas (las direcciones vienen del slice de punteros, que *sí* es secuencial), y por eso perdemos "solo" 2,6x y no las 100x del programa de latencia. En un programa real, las estructuras llenas de punteros se dispersan por el orden de asignación, el GC y el paso del tiempo: `[]*T`, listas enlazadas, árboles, `map[K]*V`.

**Ledger: 4,8 veces más rápido.** El bucle sobre el array de structs carga 64 MB para usar 8 MB. El bucle sobre el struct de arrays carga solo los 8 MB de saldos. Son 8 veces menos datos, y hay un extra: 8 MB caben en los 32 MB de L3, así que después de la primera iteración el benchmark lee sobre todo de L3 en lugar de DRAM. Tenlo en cuenta cuando leas benchmarks: **un benchmark que ejecuta la misma función en bucle sobre los mismos datos puede estar midiendo una caché caliente que producción nunca verá.**

> **¿Cuándo merece la pena un struct de arrays?** Cuando un bucle caliente toca unos pocos campos de muchos elementos, y puedes convivir con la ergonomía: añadir un elemento significa hacer append en cada slice, y pierdes `&accounts[i]`. El código que normalmente maneja un registro completo cada vez (la mayoría de la lógica de negocio) está mejor con un array de structs. El término medio habitual es partir un struct en uno pequeño "caliente" y uno "frío" al que apunta el caliente.

## Puntos Clave

- L1 ~1 ns, L2 ~3 ns, L3 ~10-15 ns, DRAM ~100+ ns: un fallo de caché hasta la RAM cuesta ~100 aciertos en L1
- La memoria se mueve en **cache lines de 64 bytes**: usa cada byte que pagas
- El acceso secuencial es rápido porque el **prefetcher** puede predecirlo; el acceso con saltos o aleatorio no
- Recorre los datos multidimensionales en el orden en que están guardados (por filas en Go)
- Prefiere `[]T` a `[]*T` para datos que recorres: los punteros los dispersan y anulan el prefetcher
- Cuando un bucle necesita un campo de muchos registros, un struct de arrays lee una fracción de la memoria
- `perf stat -e cache-misses,L1-dcache-load-misses` te da pruebas concretas en Linux

## Para Ir Más Allá (opcional)

1. En `lab_test.go`, pon `matrixN` a `256` (512 KB, cabe en L2). ¿Cuánto cuesta ahora el orden por columnas?
2. Quita el `Shuffle` de `NewPtrLedgerBefore` y vuelve a ejecutar. ¿Por qué `[]*Account` se acerca mucho más a `[]Account` cuando los punteros están en orden de asignación? ¿Duraría eso en un programa que se ejecuta durante mucho tiempo?
3. Ejecuta `cmd/latency` fijado a un core (`taskset -c 2 go run ./15-cpu-caches/cmd/latency`) y con la máquina ocupada. ¿Qué cambia?
