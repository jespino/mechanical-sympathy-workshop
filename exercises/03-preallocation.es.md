# Pre-asignación

> **¿Quieres saber más?** Lee [`runtime/slice.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/slice.go) (busca `growslice` y `nextslicecap`) y la implementación de maps con Swiss Tables en [`internal/runtime/maps`](https://github.com/golang/go/tree/go1.27.0/src/internal/runtime/maps).

Empezamos con la forma más sencilla de simpatía mecánica que existe. Si ya sabes cuánto espacio vas a necesitar, pídelo desde el principio. Parece obvio, pero `append` hace que hacer crecer un slice parezca gratis, y eso esconde una cantidad sorprendente de trabajo. En este módulo veremos qué hace `append` cuando se queda sin sitio, y después eliminaremos ese trabajo.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás qué hace `runtime.growslice` y cómo elige Go la nueva capacidad
- Entenderás por qué hacer crecer un slice cuesta asignaciones, copias *y* trabajo del GC
- Sabrás cómo crecen los maps de Go y qué te aporta un tamaño inicial en `make(map[K]V, n)`
- Reconocerás `growslice` en un perfil de CPU y lo solucionarás pre-asignando
- Sabrás cuándo basta con una cota superior, y cuándo pre-asignar es contraproducente

## El Concepto: Lo Que Hace Realmente `append`

Un slice es una cabecera de tres palabras: un puntero a un array subyacente, una longitud y una capacidad.

```go
type slice struct {
    array unsafe.Pointer
    len   int
    cap   int
}
```

Mientras `len < cap`, `append` simplemente escribe el elemento e incrementa la longitud. Es barato y nunca toca el allocator. Cuando `len == cap`, el código generado por el compilador llama a `runtime.growslice`, que:

1. **Calcula una nueva capacidad** con `nextslicecap`
2. **Reserva un nuevo array subyacente** a través de `mallocgc`
3. **Copia todos los elementos existentes** al nuevo array con `memmove`
4. **Abandona el array antiguo** como basura que el GC tendrá que encontrar y liberar

### La fórmula de crecimiento

Este es `nextslicecap`, de `runtime/slice.go`:

```go
const threshold = 256
if oldCap < threshold {
    return doublecap
}
for {
    // Transition from growing 2x for small slices
    // to growing 1.25x for large slices.
    newcap += (newcap + 3*threshold) >> 2
    if uint(newcap) >= uint(newLen) {
        break
    }
}
```

![Cada vez que append se queda sin capacidad, growslice reserva un array más grande, copia todo y deja el viejo como basura](images/03-growslice.webp)

Los slices pequeños duplican su tamaño. Los más grandes crecen por un factor que baja suavemente de 2x hacia 1.25x. Después, `growslice` redondea el número de bytes pedido **hacia arriba hasta la siguiente clase de tamaño del allocator** (estudiaremos las clases de tamaño en [Layout de Structs y Clases de Tamaño](10-struct-layout.md)), y por eso las capacidades que observas no son exactamente las que da la fórmula.

Veámoslo en acción. El laboratorio incluye un pequeño programa que imprime cada realocación:

```bash
cd labs
go run ./03-preallocation/cmd/growth
```

```
len=    1  cap     0 ->     4  (x4.00)
len=    5  cap     4 ->     8  (x2.00)
...
len=  257  cap   256 ->   512  (x2.00)
len=  513  cap   512 ->   848  (x1.66)
len=  849  cap   848 ->  1280  (x1.51)
len= 1281  cap  1280 ->  1792  (x1.40)
len= 1793  cap  1792 ->  2560  (x1.43)

appended 2000 ints (16000 bytes), copied 39520 bytes while growing
```

El programa necesitaba 16 KB de datos, pero por el camino movió **casi 40 KB** e hizo 12 asignaciones. Cada una deja un array viejo para el GC. Fíjate en `512 -> 848`: la fórmula dice 832, pero 832 × 8 = 6656 bytes no es una clase de tamaño, así que se redondeó a la clase de 6784 bytes, donde caben 848 ints.

> **O(1) amortizado no es gratis.** Duplicar hace que `append` sea de tiempo constante *amortizado*, así que la copia total está acotada por una constante multiplicada por el tamaño final. Es una garantía algorítmica, no de rendimiento. La constante sigue incluyendo asignaciones, copias, fallos de caché sobre la memoria recién reservada y trabajo del GC.

### Los maps también crecen

Desde Go 1.24, los maps son **Swiss Tables**. Claves y valores viven en *grupos* de 8 slots, cada uno con una palabra de control de 8 etiquetas de un byte (`internal/runtime/maps/group.go`). Una tabla puede llenarse hasta 7/8 de sus slots (`maxAvgGroupLoad = 7`). A partir de ahí, crece: una tabla duplica su tamaño, y cuando alcanza `maxTableCapacity` (1024 slots) se **divide** en dos bajo un directorio (`table.grow`, `table.split` en `table.go`).

![Un map Swiss Table: un directorio de tablas, cada una formada por grupos de 8 slots con una palabra de control](images/03-swiss.webp)

En cualquier caso, crecer significa reservar grupos nuevos y **reinsertar todas las entradas**, recalculando su hash. Un map que llenas desde vacío con 10.000 entradas pasa por esto varias veces.

`make(map[K]V, hint)` permite al runtime calcular desde el principio el número de grupos y tablas necesarios (mira `NewMap` en `map.go`). Pero no puede saber si la pista es acertada: el hint se *reserva*, lo uses o no.

## El Laboratorio

```
labs/03-preallocation/
  before.go      implementaciones ingenuas (referencia, no lo edites)
  after.go       tu copia: haz que esta sea rápida
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
  cmd/growth/    el programa de demostración de arriba
```

Hay tres funciones, cada una una variante del mismo problema:

| Función | Lo que sabes del tamaño final |
|---|---|
| `Squares(n)` | Exactamente `n` |
| `Evens(nums)` | Como mucho `len(nums)` |
| `CountWords(words)` | Como mucho `len(words)` claves distintas |

### Paso 1: Mide la línea base

Desde la raíz del repositorio:

```bash
make bench LAB=03
```

Esto ejecuta cada benchmark 6 veces tanto para `impl=before` como para `impl=after`, guarda la salida en bruto en `labs/03.bench.txt` e imprime una comparación con `benchstat`, con una columna por implementación. Ahora mismo `after.go` es una copia de `before.go`, así que las dos columnas deberían ser estadísticamente idénticas (`~`). Ese es tu nivel de ruido: mira los valores `±` para ver cuánto ruido tiene tu máquina.

### Paso 2: Encuentra el coste en un perfil

Los benchmarks te dicen *cuánto*, y los perfiles te dicen *dónde*:

```bash
cd labs
go test -run '^$' -bench 'Squares/impl=before' -cpuprofile cpu.prof ./03-preallocation/
go tool pprof -top -cum cpu.prof | grep -E 'growslice|mallocgc|gcBgMarkWorker|Squares'
```

```
0.17s  7.87%  7.87%   0.99s 45.83%  labs/03-preallocation.SquaresBefore
0.01s  0.46%  8.33%   0.82s 37.96%  runtime.growslice
    0     0%  8.33%   0.72s 33.33%  runtime.gcBgMarkWorker
    0     0% 29.17%   0.28s 12.96%  runtime.mallocgc
    0     0% 77.78%   0.03s  1.39%  runtime.mallocgcSmallNoScanSC6
```

**Más del 80% del tiempo de `SquaresBefore` se va dentro de `growslice`**, no calculando cuadrados. Y un tercio de todo el proceso son los workers de marcado en segundo plano del GC limpiando los arrays abandonados. Ese coste ni siquiera aparece bajo el nombre de tu función. (Novedad de Go 1.27: `mallocgcSmallNoScanSC6` es una de las rutinas de reserva *especializadas por tamaño*, una por clase de tamaño, a las que ahora llaman `mallocgc` y el código generado por el compilador para las reservas pequeñas. Abaratan un poco las reservas pequeñas, pero no eliminan las copias ni el trabajo del GC.)

Prueba `go tool pprof -http=: cpu.prof` y abre el flame graph para ver lo mismo de forma visual.

### Paso 3: Arréglalo

Edita `labs/03-preallocation/after.go`:

1. **`SquaresAfter`**: el tamaño es exactamente `n`. Reserva una vez con `make([]int, n)` y escribe por índice (o `make([]int, 0, n)` y sigue usando `append`).
2. **`EvensAfter`**: no conoces el tamaño exacto, pero `len(nums)` es una cota superior. Usa `make([]int, 0, len(nums))`.
3. **`CountWordsAfter`**: dale al map un tamaño inicial con `make(map[string]int, len(words))`.

`make test-labs` (o `go test ./03-preallocation/`) comprueba que los resultados no han cambiado.

### Paso 4: Compara

```bash
make bench LAB=03
```

Estos son los resultados con la solución de referencia (`make solution LAB=03`) en un AMD Ryzen AI MAX+ 395. Tus números serán distintos, pero la forma debería ser la misma:

```
              │    before     │                after                │
              │    sec/op     │    sec/op     vs base               │
Squares-32      28.684µ ± 30%   6.690µ ± 13%  -76.68% (p=0.002 n=6)
Evens-32        13.415µ ±  6%   7.069µ ±  7%  -47.31% (p=0.002 n=6)
CountWords-32    252.8µ ± 17%   152.6µ ± 10%  -39.63% (p=0.002 n=6)

              │    before     │                after                │
              │     B/op      │     B/op      vs base               │
Squares-32      349.19Ki ± 0%   80.00Ki ± 0%  -77.09% (p=0.002 n=6)
Evens-32        125.19Ki ± 0%   80.00Ki ± 0%  -36.10% (p=0.002 n=6)
CountWords-32    426.4Ki ± 0%   426.7Ki ± 0%   +0.06% (p=0.002 n=6)

              │   before    │               after               │
              │  allocs/op  │ allocs/op   vs base               │
Squares-32      16.000 ± 0%   1.000 ± 0%  -93.75% (p=0.002 n=6)
Evens-32        13.000 ± 0%   1.000 ± 0%  -92.31% (p=0.002 n=6)
CountWords-32    48.00 ± 0%   34.00 ± 0%  -29.17% (p=0.002 n=6)
```

## Entendiendo los Resultados

**Squares** es el caso limpio: 16 asignaciones pasan a ser 1, y 349 KB de memoria reservada en total pasan a ser exactamente los 80 KB que necesitamos (10.000 × 8 bytes, redondeado a páginas completas).

**Evens** es interesante. Solo la mitad de los números son pares, así que la respuesta *exacta* necesita 40 KB, y hemos reservado 80 KB. Hemos cambiado memoria por velocidad: una asignación en lugar de 13, y ninguna copia. Suele ser un buen trato cuando la cota superior se parece al tamaño real. Es un mal trato cuando la cota es exageradamente pesimista, por ejemplo reservar `len(input)` para un filtro que se queda con el 0,1%.

**CountWords** tiene el mismo matiz con los maps. Solo hay 5.000 palabras distintas entre 10.000 entradas, así que el hint reserva el doble de lo necesario. Por eso la columna B/op no se ha movido nada: los bytes que ahorramos al no crecer se gastaron en slots que nunca usamos. Aun así, el tiempo bajó un 40%, porque no hubo ningún rehash. ¿Por qué siguen quedando 34 asignaciones? Las claves son strings que ya estaban reservados, así que no son asignaciones nuevas. Lo que queda es el directorio, las tablas y los grupos de un map grande, reservados una sola vez cada uno.

> **Regla práctica:** pre-asigna cuando conozcas el tamaño exacto, o tengas una cota superior que normalmente se le acerque. Si la cota puede fallar por órdenes de magnitud, usa una estimación razonable o deja que `append` haga su trabajo.

## Más Allá de Slices y Maps

La misma idea aparece por toda la librería estándar:

- `strings.Builder.Grow(n)` y `bytes.Buffer.Grow(n)`: dimensiona un buffer que vas a llenar (más en [Strings y Bytes](04-strings-bytes.md))
- `slices.Grow(s, n)`: asegura que `s` tiene sitio para `n` elementos más sin cambiar su longitud
- `append(dst[:0], ...)`: reutiliza un array subyacente existente en lugar de reservar uno nuevo
- `slices.Collect` y `maps.Collect` sobre un iterador no pueden conocer el tamaño, mientras que `make` + bucle sí

## Puntos Clave

- `append` sobre un slice lleno llama a `growslice`: **reserva, copia todo y deja basura atrás**
- El crecimiento es de 2x para slices pequeños, bajando hacia 1.25x para los grandes, redondeado a una clase de tamaño
- Los maps (Swiss Tables) crecen duplicando o dividiendo tablas, y cada crecimiento recalcula el hash de todas las entradas
- Un tamaño conocido, o una cota superior ajustada, convierte N asignaciones en 1
- La mejora aparece en **allocs/op**, en **sec/op** y en tiempo de GC que se carga a otras goroutines
- Un hint demasiado generoso desperdicia memoria, así que mide B/op y no solo sec/op

## Para Ir Más Allá (opcional)

1. Cambia `size` en `lab_test.go` a `100` y a `1_000_000`. ¿Cómo cambia la mejora? ¿Por qué?
2. En `EvensAfter`, prueba `slices.Clip` sobre el resultado. ¿Cuánto cuesta y cuándo te interesaría?
3. Ejecuta los benchmarks con `GODEBUG=gctrace=1` y compara cuántos ciclos de GC provoca cada implementación.
