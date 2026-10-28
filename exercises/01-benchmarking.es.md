# Benchmarking 101

Todos los módulos de este workshop terminan con la misma frase: *mídelo*. Así que antes de optimizar nada necesitamos saber medir. Go trae una buena herramienta para eso en el paquete `testing`. En este módulo vemos cómo escribir y ejecutar un benchmark, cómo usar `benchstat` para averiguar si un cambio en el código ha marcado alguna diferencia y cómo funcionan los laboratorios de este workshop.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás escribir un benchmark con `b.Loop`, y qué hace por ti
- Sabrás ejecutar un benchmark y leer su salida
- Usarás `benchstat` para comparar dos versiones de tu código con estadística en lugar de a ojo
- Sabrás cómo están organizados los laboratorios del resto del workshop

## Cómo Ejecuta `go test -bench` Tu Código

### Escribir un benchmark con `b.Loop`

Un benchmark es una función cuyo nombre empieza por `Benchmark`, que recibe un `*testing.B` y pone el código a medir dentro de un bucle `for b.Loop()`:

```go
func BenchmarkFoo(b *testing.B) {
    // preparación: se ejecuta una vez, no se mide
    for b.Loop() {
        Foo()
    }
}
```

`b.Loop` (Go 1.24+) se encarga de las partes difíciles por ti:

- **Decide cuántas iteraciones ejecutar.** Mantiene el bucle en marcha hasta que ha corrido lo suficiente para dar un número estable (1 segundo por defecto, `-benchtime` lo cambia), y muestra el tiempo total dividido entre el número de iteraciones: la columna `ns/op`.
- **La preparación nunca se mide.** Lo que hagas antes del bucle se ejecuta una vez, y el cronómetro arranca en la primera llamada a `b.Loop()`.
- **El cuerpo del bucle se mantiene vivo**, así que el compilador no puede tirar una llamada solo porque no uses su resultado.

> **La forma antigua.** Todavía verás benchmarks escritos como `for i := 0; i < b.N; i++ { ... }`. Funcionan, pero tienes que llamar a `b.ResetTimer()` después de la preparación y asegurarte de que el compilador no tire tu código. `b.Loop` hace ambas cosas por ti, y es la forma recomendada desde Go 1.24.

### Ejecutar un benchmark

Los benchmarks se ejecutan con `go test -bench`. Desde el directorio `labs/`:

```bash
go test -run '^$' -bench Stats -benchmem ./01-benchmarking/
```

`-bench Stats` elige qué benchmarks ejecutar (una expresión regular, `.` para todos), `-run '^$'` se salta los tests normales y `-benchmem` añade estadísticas de memoria. Cada resultado es una línea:

```
BenchmarkStats/impl=before-32    	  305234	      3953 ns/op	       0 B/op	       0 allocs/op
```

Es el nombre (`-32` es el número de CPUs que podía usar), el número de iteraciones, el tiempo por iteración, y los bytes y asignaciones por iteración.

Dos flags más que verás: `-count 6` ejecuta cada benchmark 6 veces, y `-benchtime 2s` hace que cada ejecución dure más. En los laboratorios, `make bench LAB=NN` hace todo esto por ti.

### Por qué `benchstat`

Una sola ejecución no te dice casi nada: en un portátil con el navegador abierto (o, en este workshop, con los benchmarks de otras personas ejecutándose) el mismo código puede variar un 10% o más. [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) hace las cuentas por ti. Toma varias ejecuciones de cada benchmark y muestra el tiempo típico (la **mediana**) y cuánto variaron las ejecuciones (`± 4%`). Cuando comparas dos conjuntos de ejecuciones, también te dice si la diferencia es real o podría ser solo ruido. Si podría ser ruido, imprime `~` en lugar de un porcentaje. El valor `p=` que aparece al lado es la probabilidad de ver una diferencia así de grande solo por suerte. Por debajo de 0,05 se considera una diferencia real.

Para comparar dos versiones de tu propio código, guarda los resultados antes y después del cambio y pásale los dos ficheros a `benchstat`:

```bash
go test -run '^$' -bench . -count 6 > old.txt
# ...cambia el código...
go test -run '^$' -bench . -count 6 > new.txt
benchstat old.txt new.txt
```

### Cómo funcionan los laboratorios

Casi todos los laboratorios, empezando por este, tienen los mismos ficheros en `labs/NN-nombre/`:

- `before.go`: la versión lenta. No lo edites.
- `after.go`: tu copia. Empieza idéntica a `before.go` (sus funciones terminan en `After`), y es el fichero que cambias.
- `solution.go`: una solución de referencia, por si te atascas.
- `lab_test.go`: un test que comprueba que tu versión sigue dando los mismos resultados, y los benchmarks.

Cada benchmark mide las dos versiones en la misma ejecución, como dos sub-benchmarks llamados `impl=before` e `impl=after`. Se ejecutan con:

```bash
make bench LAB=03
```

Esto ejecuta los benchmarks del laboratorio 6 veces, guarda la salida en `labs/03.bench.txt` y después ejecuta `benchstat -col /impl`, que pone `before` y `after` lado a lado y los compara:

```
              │    before     │                after                │
              │    sec/op     │    sec/op     vs base               │
Squares-32      28.684µ ± 30%   6.690µ ± 13%  -76.68% (p=0.002 n=6)
```

No hay que guardar ninguna línea base: edita `after.go` y vuelve a ejecutar `make bench`. `make solution LAB=03` hace lo mismo con la solución de referencia en lugar de tu copia. Para ejecutar o perfilar solo una de las dos, añádela al nombre del benchmark, por ejemplo `-bench 'Squares/impl=before'`.

## El Laboratorio

Un primer laboratorio, pequeño a propósito, para acostumbrarte al ciclo que siguen todos los módulos. En `labs/01-benchmarking/`:

```
lab.go        Sum, que suma un slice de ints
before.go     StatsBefore (no lo edites)
after.go      StatsAfter: tu copia, la que cambias
solution.go   solución de referencia (se compila con -tags solution)
lab_test.go   el test de corrección y BenchmarkStats
```

`StatsBefore` devuelve la suma y la media de un slice:

```go
func StatsBefore(nums []int) (sum, avg int) {
	return Sum(nums), Sum(nums) / len(nums)
}
```

Funciona, pero llama a `Sum` dos veces con el mismo slice, así que suma los 10.000 números dos veces.

### Paso 1: Mide la línea base

Desde la raíz del repositorio:

```bash
make bench LAB=01
```

`after.go` todavía es una copia de `before.go`, así que las dos columnas deberían ser iguales (`~`):

```
         │   before    │            after             │
         │   sec/op    │   sec/op     vs base         │
Stats-32   3.966µ ± 1%   3.995µ ± 2%  ~ (p=0.180 n=6)
```

Ese es tu nivel de ruido: los valores `±` te dicen cuánto ruido tiene tu máquina.

### Paso 2: Arréglalo

Edita `labs/01-benchmarking/after.go` para que `StatsAfter` llame a `Sum` una sola vez y reutilice el resultado para la media. Después comprueba que sigue dando los mismos resultados:

```bash
cd labs
go test ./01-benchmarking/
```

### Paso 3: Compara

```bash
make bench LAB=01
```

Estos son los resultados con la solución de referencia (`make solution LAB=01`) en un AMD Ryzen AI MAX+ 395:

```
         │   before    │               after                │
         │   sec/op    │   sec/op     vs base               │
Stats-32   4.006µ ± 2%   1.999µ ± 2%  -50.11% (p=0.002 n=6)
```

## Entendiendo los Resultados

La mitad de trabajo, la mitad de tiempo: `before` suma el slice dos veces y `after` solo una, así que pasa de unos 4 µs a unos 2 µs. `benchstat` lo muestra como `-50.11%`, y `p=0.002` dice que es muy poco probable que una diferencia así de grande sea suerte.

Tus números serán distintos, pero la proporción debería estar cerca de 2x. Si `benchstat` imprime `~`, las ejecuciones tuvieron demasiado ruido para saberlo: cierra otros programas y vuelve a ejecutarlo.

## Puntos Clave

- Un benchmark es una función `BenchmarkXxx(b *testing.B)` con el código a medir dentro de `for b.Loop()`
- `b.Loop` elige por ti el número de iteraciones, nunca cronometra la preparación y mantiene vivos los resultados de las llamadas
- Una sola ejecución no te dice casi nada: usa `-count 6` (o más) y `benchstat`
- `~` significa "no se distingue del ruido", y es una respuesta perfectamente válida
- En cada laboratorio: edita `after.go`, ejecuta `make bench LAB=NN` y compara las dos columnas

## Para Ir Más Allá (opcional)

1. Escribe tu propio benchmark desde cero, fuera de la maquinaria del workshop: un directorio nuevo con un `go.mod`, una función pequeña y un fichero `_test.go` con un `BenchmarkXxx` que use `b.Loop`.
2. Ejecútalo con `go test -bench .` y juega con los flags: `-count`, `-benchtime`, `-benchmem`. ¿Cuánto cambian los números de una ejecución a otra?
3. Guarda una ejecución en `old.txt`, cambia la función, guarda otra ejecución en `new.txt` y compáralas con `benchstat old.txt new.txt`.
