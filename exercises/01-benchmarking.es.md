# Benchmarking 101

> **¿Quieres saber más?** Lee [`testing/benchmark.go`](https://github.com/golang/go/blob/go1.27.0/src/testing/benchmark.go) (busca `launch`, `predictN`, `runN` y `Loop`) y el pase del compilador que hace funcionar `b.Loop`, [`cmd/compile/internal/bloop/bloop.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/bloop/bloop.go).

Todos los módulos de este workshop terminan con la misma frase: *mídelo*. Así que antes de optimizar nada necesitamos una herramienta de medida en la que podamos confiar. Go trae una buena en `testing`, pero es sorprendentemente fácil escribir un benchmark que compila, se ejecuta, imprime un número creíble... y no mide nada en absoluto. En este módulo veremos cómo ejecuta `go test -bench` tu código, cómo el compilador puede borrar el código que querías medir y cómo comparar resultados sin engañarte a ti mismo.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás cómo elige `testing.B` el valor de `b.N` y cuántas veces llama a tu función de benchmark
- Entenderás qué garantiza `b.Loop` (y qué no), hasta llegar al pase del compilador que lo implementa
- Reconocerás las tres mentiras clásicas: eliminación de código muerto, plegado de constantes y un benchmark que modifica su propia entrada
- Sabrás qué hacen `-benchmem`, `-count`, `-cpu` y `-benchtime`, y lo que cuestan realmente `ResetTimer` / `StopTimer`
- Usarás `benchstat` para comparar ejecuciones con estadística en lugar de a ojo

## El Concepto: Cómo Ejecuta `go test -bench` Tu Código

### La rampa de `b.N`

El benchmark clásico es un bucle hasta `b.N`:

```go
func BenchmarkFoo(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Foo()
    }
}
```

![Cómo go test aumenta b.N hasta que una ejecución dura lo suficiente para medirla](images/01-bn-ramp.webp)

Tú no eliges `b.N`. El framework ejecuta tu función con `b.N = 1`, mide cuánto ha tardado y predice cuántas iteraciones necesita para llenar el tiempo objetivo (`-benchtime`, 1s por defecto). Esta es `predictN`, de `testing/benchmark.go`:

```go
n := goalns * prevIters / prevns
// Run more iterations than we think we'll need (1.2x).
n += n / 5
// Don't grow too fast in case we had timing errors previously.
n = min(n, 100*last)
// Be sure to run at least one more than last time.
n = max(n, last+1)
// Don't run more than 1e9 times. (This also keeps n in int range on 32 bit platforms.)
n = min(n, maxBenchPredictIters)
```

`launch` la llama en un bucle hasta que la duración medida alcanza el objetivo. Cada ronda pasa por `runN`, que primero llama a `runtime.GC()` ("by clearing garbage from previous runs"), reinicia el temporizador y llama a tu función **desde el principio**. Puedes verlo:

```
BenchmarkCallsN called with b.N = 1
BenchmarkCallsN called with b.N = 100
BenchmarkCallsN called with b.N = 10000
BenchmarkCallsN called with b.N = 1000000
BenchmarkCallsN called with b.N = 100000000
BenchmarkCallsN called with b.N = 1000000000
```

Dos consecuencias. El **número que se muestra es solo el de la última ronda** (`ns/op = duración / b.N`). Y cualquier preparación que hagas antes del bucle se ejecuta **seis veces**, y se cronometra salvo que llames a `b.ResetTimer()` después. Olvidarlo puede ser dramático: un benchmark de búsqueda en un map que primero construye un map de 2 millones de entradas dio 13–19 ns/op sin `ResetTimer` y 8,6–10,7 ns/op con él (AMD Ryzen AI MAX+ 395). Mismo código, misma búsqueda.

### `b.Loop` (Go 1.24+)

Go 1.24 añadió una forma mejor de escribir el bucle:

```go
func BenchmarkFoo(b *testing.B) {
    // preparación: no se mide
    for b.Loop() {
        Foo()
    }
}
```

`Loop` hace su propia rampa *dentro de una sola llamada* a tu función. `loopSlowPath` reinicia el temporizador en la primera llamada (así la preparación nunca se mide) y llama a `stopOrScaleBLoop`, que reutiliza `predictN`, cada vez que se alcanza el objetivo actual. `launch` ve que `b.loop.n` tiene valor y no vuelve a llamar a tu función:

```
BenchmarkCallsLoop called with b.N = 1
  after loop b.N = 1000000000
```

El camino rápido es lo bastante pequeño como para hacer inlining dentro de tu benchmark:

```go
func (b *B) Loop() bool {
    if b.loop.i < b.loop.n {
        b.loop.i++
        return true
    }
    return b.loopSlowPath()
}
```

Fíjate en que `b.N` vale 0 mientras el bucle se ejecuta (así que no lo uses dentro), y recibe el número total de iteraciones cuando `Loop` devuelve false.

### Eliminación de código muerto: el benchmark que no mide nada

Este es el problema que `b.Loop` vino realmente a resolver. Si `Foo` es pequeña, el compilador le hace inlining. Si su resultado no se usa y no tiene efectos secundarios, el compilador la borra. Lo que queda es un bucle vacío. Este es el desensamblado de un bucle `b.N` alrededor de `PopCount(uint64(i))` del laboratorio (`go tool objdump`):

```
before_test.go:14   XORL CX, CX
before_test.go:14   JMP 0x544167
before_test.go:14   INCQ CX
before_test.go:14   CMPQ 0x210(AX), CX     // i < b.N
before_test.go:14   JG 0x544164
before_test.go:17   RET
```

![Eliminación de código muerto: el compilador elimina el trabajo cuyo resultado no se usa](images/01-dce.webp)

Ni rastro del popcount. El benchmark da **0,2 ns/op**, que es más o menos un ciclo de reloj por iteración en esta CPU. Ese número es tu alarma: casi nada útil tarda menos de un nanosegundo.

### Lo que `b.Loop` garantiza de verdad

La documentación de `Loop` dice:

> Within the body of a "for b.Loop() { ... }" loop, arguments to and results from function calls and assigned variables within the loop are kept alive, preventing the compiler from fully optimizing away the loop body. Currently, this is implemented as a compiler transformation that wraps such variables with a runtime.KeepAlive intrinsic call.

Esa transformación vive en `cmd/compile/internal/bloop`. Se ejecuta **antes del inlining** (mira `gc/main.go`: `bloop.Walk` se llama justo antes de `DevirtualizeAndInlinePackage`) y reescribe cada sentencia del cuerpo del bucle. Para una llamada con resultados, los asigna a temporales y los mantiene vivos:

```go
// preserveCallResults assigns the results of a call statement to temporary variables
// to ensure they remain alive.
```

Puedes verlo con `-gcflags=-m=2`:

```
./before_test.go:23:6: function result will be kept alive
```

Merece la pena conocer tres cosas, porque *no* son lo que dicen la mayoría de los artículos:

1. **En Go 1.27, las llamadas dentro de `b.Loop` siguen recibiendo inlining.** (La primera implementación, en Go 1.24, mantenía vivo el cuerpo negándose a hacer inlining de sus llamadas; el compilador actual usa la reescritura con KeepAlive.) `-gcflags=-m` imprime `inlining call to Mix` para la llamada dentro del cuerpo de `b.Loop`.
2. **Mantener vivo el resultado no mantiene vivo el cálculo.** Si las entradas son constantes, la función con inlining se *pliega como constante*, y KeepAlive mantiene vivo, tan contento, un valor constante. En el laboratorio, `for b.Loop() { Mix(42) }` se compila a:

   ```
   before_test.go:23   MOVQ $0xa759ea27d4727622, CX   // Mix(42), calculado al compilar
   before_test.go:23   MOVQ CX, 0x8(SP)               // el KeepAlive
   ```

   Lo mismo pasa con `v := uint64(42)` declarada antes del bucle: el compilador propaga la constante.
3. **Solo se aplica a las sentencias que están sintácticamente entre las llaves**, y la condición debe ser literalmente `b.Loop()`. Y cambia tu código: las variables asignadas se mantienen vivas, lo que puede desactivar optimizaciones que querías medir. Por ejemplo, `k := string(buf); _ = m[k]` no asigna memoria en una función normal (el compilador reutiliza los bytes de `buf` para la búsqueda), pero asigna **48 B/op dentro del cuerpo de un `b.Loop`**, porque ahora `k` tiene que mantenerse vivo. Ante la duda, pon el código bajo prueba en una función y llámala desde el bucle.

### El benchmark que modifica su propia entrada

El compilador no es el único que te puede mentir. Si la operación modifica su entrada, cada iteración después de la primera mide otra cosa. Ordenar un slice in situ es el caso clásico: a partir de la segunda iteración estás cronometrando `slices.Sort` sobre datos ya ordenados, algo que pdqsort detecta y termina en tiempo lineal. La solución es restaurar la entrada en cada iteración (`copy` desde un slice intacto) y comprobar que restaurarla es barato comparado con la operación.

¿Y usar `b.StopTimer()` / `b.StartTimer()` alrededor de la restauración? Funcionan, pero mira lo que hacen:

```go
func (b *B) StopTimer() {
    if b.timerOn {
        b.duration += highPrecisionTimeSince(b.start)
        runtime.ReadMemStats(&memStats)
        ...
```

`runtime.ReadMemStats` **para el mundo** (stop the world). Llamarla dos veces por iteración no importa en una ordenación de 400 µs, pero es terrible para operaciones rápidas: un `PopCount` de ~1 ns envuelto en `StopTimer`/`StartTimer` dio 27,51 ns/op, y 1.000.000 de iteraciones tardaron 16,3 s de tiempo real. Con el `-benchtime=1s` por defecto, el mismo benchmark estuvo **más de 8 minutos** ejecutándose antes de que lo matáramos.

### Los flags que usarás todo el rato

| Flag | Qué hace |
|---|---|
| `-bench=regexp` | Qué benchmarks ejecutar (`.` para todos). Combínalo con `-run='^$'` para saltarte los tests |
| `-benchmem` | Muestra `B/op` y `allocs/op` (a partir de `runtime.ReadMemStats` en `StartTimer`/`StopTimer`). Equivale a llamar a `b.ReportAllocs()` |
| `-count=N` | Ejecuta cada benchmark N veces. Necesitas varias muestras para hacer estadística |
| `-benchtime=2s` o `=1000x` | Duración objetivo por benchmark, o un número exacto de iteraciones |
| `-cpu=1,4,8` | Ejecuta con cada valor de `GOMAXPROCS`. Es el sufijo `-32` de `BenchmarkFoo-32` |

`b.SetBytes(n)` añade una columna `MB/s`, y `b.ReportMetric(v, "unidad")` añade las tuyas propias.

### Por qué `benchstat`

Una sola ejecución no te dice casi nada: en un portátil con el navegador abierto (o, en este workshop, con los benchmarks de otras personas ejecutándose) el mismo código puede variar un 10% o más. [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) toma varias muestras por benchmark y muestra la **mediana** con un **intervalo de confianza** (`± 4%`), y compara dos conjuntos con un **test U de Mann-Whitney**, un test que no supone que los tiempos sigan una distribución normal. Si la diferencia podría ser ruido, imprime `~` y el p-valor en lugar de un porcentaje. Con `-count=6`, el p-valor más pequeño que puede alcanzar es 0,002, y cualquier cosa por debajo de 0,05 se considera significativa.

El flujo básico, que también usarán los módulos siguientes:

```bash
go test -run '^$' -bench . -count 6 > old.txt
# ...cambia el código...
go test -run '^$' -bench . -count 6 > new.txt
benchstat old.txt new.txt
```

Los laboratorios usan una variante: las dos implementaciones se ejecutan en el mismo fichero como sub-benchmarks llamados `impl=before` e `impl=after`, y `benchstat -col /impl` convierte esa clave en columnas.

## El Laboratorio

Este laboratorio es distinto de los demás. El código bajo prueba está bien. **Los benchmarks están rotos**, y tú los arreglas.

```
labs/01-benchmarking/
  lab.go             PopCount y Mix, las funciones que medimos (no lo edites)
  before_test.go     tres benchmarks rotos (línea base, no lo edites)
  after_test.go      tu copia: arregla estos
  solution_test.go   solución de referencia (se compila con -tags solution)
  lab_test.go        tests de corrección + las funciones Benchmark*
```

| Benchmark | Qué debería medir | Qué falla |
|---|---|---|
| `PopCount` | Una llamada a `PopCount` | Bucle `b.N`, resultado descartado |
| `Mix` | Una llamada a `Mix` | `b.Loop`, pero la entrada es una constante |
| `Sort` | Ordenar 10.000 ints aleatorios | Ordena el mismo slice in situ |

### Paso 1: Mide la línea base

```bash
make bench LAB=01
```

Las dos columnas ejecutan el mismo código roto, así que esperarías `~` en todo. Muchas veces lo verás, pero no siempre. En una de nuestras ejecuciones:

```
            │    before     │    after      vs base               │
PopCount-32   0.2051n ±  2%   0.2028n ± 2%        ~ (p=0.167 n=6)
Mix-32        0.4075n ±  2%   0.2197n ± 4%  -46.09% (p=0.002 n=6)
Sort-32        3.420µ ± 10%    3.387µ ± 6%        ~ (p=0.900 n=6)
```

Código idéntico y una diferencia "significativa" del 46%. A escala de menos de un nanosegundo, un bucle de tres o cuatro instrucciones es sensible a cosas como dónde queda en memoria (el alineamiento del código) y qué se ejecuta en el otro hilo del mismo núcleo (hyper-threading). `benchstat` puede decirte que una diferencia es real; no puede decirte que la causa sea *tu cambio*. Un motivo más para desconfiar de los resultados por debajo del nanosegundo.

Ahora mira los números y pregúntate cuáles son creíbles. Ordenar 10.000 ints en 3 µs son 0,3 ns por elemento. ¿Es eso plausible?

### Paso 2: Busca las pruebas

No te fíes de mí: mira lo que ha hecho el compilador.

```bash
cd labs
go test -c -o /tmp/b01.test ./01-benchmarking/
go tool objdump -s 'benchPopCountBefore|benchMixBefore' /tmp/b01.test
go test -run '^$' -gcflags=-m=2 ./01-benchmarking/ 2>&1 | grep -E 'before_test.*(inlining|kept alive)'
```

Deberías encontrar el bucle vacío y el `MOVQ $0xa759ea27d4727622` precalculado que vimos arriba. Para `Sort`, perfílalo (aprenderemos profiling como es debido en [Profiling 101](02-profiling.md)):

```bash
go test -run '^$' -bench 'Sort/impl=before' -cpuprofile /tmp/cpu.prof ./01-benchmarking/
go tool pprof -top /tmp/cpu.prof | head -15
```

Fíjate en qué funciones de pdqsort aparecen. Con la entrada ordenada verás sobre todo el camino barato de "¿ya está ordenado?" (`partialInsertionSortOrdered`) en lugar de trabajo real de particionado.

### Paso 3: Arregla los benchmarks

Edita `labs/01-benchmarking/after_test.go`:

1. **`benchPopCountAfter`**: cámbialo a `for b.Loop()`. Ahora el resultado de la llamada se mantiene vivo.
2. **`benchMixAfter`**: dale a `Mix` una entrada que el compilador no pueda conocer: declara `x` antes del bucle e increméntala en el cuerpo (`Mix(x); x++`).
3. **`benchSortAfter`**: mantén intactos los datos aleatorios. Reserva un slice `work` una vez y, en cada iteración, haz `copy(work, data)` y ordena `work`. Para comprobar que la copia no domina, escribe un benchmark rápido que solo copie (nosotros medimos 0,68 µs, menos del 0,2% de la ordenación).

`go test ./01-benchmarking/` comprueba que las funciones siguen funcionando.

### Paso 4: Compara

```bash
make bench LAB=01
```

Estos son los resultados con la solución de referencia (`make solution LAB=01`) en un AMD Ryzen AI MAX+ 395:

```
            │    before    │                  after                  │
            │    sec/op    │    sec/op      vs base                  │
PopCount-32   0.2024n ± 3%    0.9342n ± 2%    +361.65% (p=0.002 n=6)
Mix-32        0.2233n ± 4%    0.6875n ± 1%    +207.79% (p=0.002 n=6)
Sort-32        3.006µ ± 2%   362.797µ ± 4%  +11969.10% (p=0.002 n=6)
```

Por una vez, **que "after" sea más lento es la buena noticia**: esos son los costes reales.

### Paso 5: Practica el flujo con ficheros de línea base

Vamos a usar `benchstat` como lo usarás con tu propio código. Guarda una línea base de tu benchmark `Sort` ya arreglado:

```bash
cd labs
go test -run '^$' -bench 'Sort/impl=after' -count 6 ./01-benchmarking/ > old.txt
```

Ahora, en `benchSortAfter`, cambia `slices.Sort(work)` por `slices.SortFunc(work, cmp.Compare[int])` (añade el import de `cmp`) y vuelve a ejecutarlo hacia `new.txt`:

```bash
go test -run '^$' -bench 'Sort/impl=after' -count 6 ./01-benchmarking/ > new.txt
benchstat old.txt new.txt
```

```
                   │   old.txt   │              new.txt               │
                   │   sec/op    │   sec/op     vs base               │
Sort/impl=after-32   395.0µ ± 2%   611.7µ ± 4%  +54.84% (p=0.002 n=6)
```

Después deshaz el cambio y ejecútalo otra vez. Deberías obtener algo como `~ (p=0.589 n=6)`: el mismo código, medido dos veces, con diferencias que son solo ruido. Borra `old.txt` y `new.txt` cuando termines.

## Entendiendo los Resultados

**PopCount**: 0,2 ns/op era el coste de incrementar un contador. El popcount de verdad cuesta alrededor de 1 ns. Con `b.Loop`, el código con inlining sigue ahí, pero ahora su resultado se guarda para el KeepAlive, así que el compilador no puede descartarlo.

**Mix**: 0,22 ns/op era el coste de la contabilidad de `b.Loop` más guardar una constante. Fíjate en que el número honesto sigue por debajo del nanosegundo: `Mix` es un puñado de desplazamientos, XORs y dos multiplicaciones, y las iteraciones no dependen unas de otras (cada una usa `x`, no el resultado anterior), así que la CPU solapa varias. Eso mide **throughput**, no latencia. Si quieres latencia, pasa cada resultado a la siguiente llamada (`x = Mix(x)`). Las dos preguntas son válidas, así que asegúrate de saber cuál estás haciendo.

**Sort**: una diferencia de 120x. La versión "before" medía el mejor caso de pdqsort con datos ordenados. Bugs así sobreviven a las revisiones de código porque el número no es obviamente absurdo: 3 µs "suena rápido". Comprueba siempre que tenga sentido: 10.000 elementos × log₂(10.000) ≈ 130.000 comparaciones no caben en 3 µs.

> **Regla práctica:** si un resultado está por debajo de ~1 ns/op, o mejoró mucho tras un cambio que no esperabas que importara, mira el ensamblador antes de celebrarlo.

## Puntos Clave

- Con un bucle `b.N`, tu función se llama varias veces con un `b.N` creciente. La preparación anterior al bucle se cronometra salvo que llames a `b.ResetTimer()`
- `b.Loop` ejecuta tu función una vez, nunca cronometra la preparación y mantiene vivos los resultados de las llamadas y las variables asignadas mediante una reescritura del compilador
- `b.Loop` **no** impide el inlining ni el plegado de constantes. Las entradas no pueden ser constantes conocidas al compilar
- La reescritura con KeepAlive cambia el código dentro del bucle. Pon el código bajo prueba en una función
- Los benchmarks que modifican su entrada deben restaurarla en cada iteración. `StopTimer`/`StartTimer` paran el mundo, así que evítalos en bucles rápidos
- Usa `-count 6` (o más) y `benchstat`. `~` significa "no se distingue del ruido", y es una respuesta perfectamente válida

## Para Ir Más Allá (opcional)

1. Escribe una versión de latencia del benchmark de `Mix` (`x = Mix(x)`). ¿Cómo se compara con la versión de throughput, y por qué?
2. Ejecuta el benchmark de `PopCount` con `-cpu 1,8,32`. ¿Cambia `GOMAXPROCS` el resultado de un benchmark de una sola goroutine? Ahora prueba `b.RunParallel`.
3. Añade `b.SetBytes(10_000 * 8)` al benchmark de `Sort` y mira la nueva columna de MB/s.
