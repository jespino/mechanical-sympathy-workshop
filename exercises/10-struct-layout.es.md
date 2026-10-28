# Layout de Structs y Clases de Tamaño

> **¿Quieres saber más?** Lee cómo coloca el compilador los campos de un struct en [`cmd/compile/internal/types/size.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/types/size.go) (busca `calcStructOffset` y `CalcStructSize`), la tabla de clases de tamaño del allocator en [`internal/runtime/gc/sizeclasses.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/runtime/gc/sizeclasses.go), y el analizador [`fieldalignment`](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/fieldalignment). La sección de la especificación de Go sobre [garantías de tamaño y alineamiento](https://go.dev/ref/spec#Size_and_alignment_guarantees) es corta y merece la pena.

Este es el primer módulo de la Parte III, donde pasamos del compilador al runtime, empezando por su allocator. Empezamos por algo que controlas por completo: el orden de los campos de tus structs. Go nunca reordena los campos por ti. Si los pones en mal orden, el compilador añade relleno (padding) invisible, el allocator vuelve a redondear el resultado hacia arriba, y cada bucle que recorre tus datos lee más memoria de la necesaria.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Conocerás las reglas de alineamiento de Go y sabrás leerlas con `unsafe.Sizeof`, `unsafe.Alignof` y `unsafe.Offsetof`
- Entenderás de dónde sale el padding y cómo ordenar los campos de mayor a menor lo elimina
- Conocerás la trampa del campo de tamaño cero al final
- Sabrás cómo el allocator redondea cada objeto pequeño a una **clase de tamaño**
- Usarás el analizador `fieldalignment` para encontrar structs con desperdicio automáticamente
- Medirás el efecto de un struct más pequeño en las asignaciones y en la velocidad de recorrido

## El Concepto: Padding, Alineamiento y Clases de Tamaño

### Reglas de alineamiento

Cada tipo tiene un **alineamiento**: su dirección tiene que ser múltiplo de ese número. En amd64 y arm64, un valor de 1, 2, 4 u 8 bytes se alinea a su propio tamaño (`bool`, `uint16`, `uint32`, `int64`, `float64`, punteros…). Un struct se alinea al mayor alineamiento de sus campos, y un array al alineamiento de su elemento.

El compilador coloca los campos **en el orden en que los declaras**, y antes de cada campo inserta el padding justo para llegar al alineamiento de ese campo. Este es el núcleo de `calcStructOffset` en `cmd/compile/internal/types/size.go`:

```go
for _, f := range fields {
    CalcSize(f.Type)
    offset = RoundUp(offset, int64(f.Type.align))
    ...
    f.Offset = offset
    offset += f.Type.width
```

Después, `CalcStructSize` redondea el total al alineamiento del struct, para que en un array todos los elementos queden alineados también:

```go
size = RoundUp(size, int64(maxAlign))
```

Así que el padding aparece en dos sitios: **antes de un campo** que necesita más alineamiento del que da el offset actual, y **al final** del struct.

### Verlo con tus propios ojos

Fíjate en la partícula del laboratorio. Sus campos están en un orden "lógico", con cada flag junto al valor que describe:

```go
type ParticleBefore struct {
    Active bool
    X      float64
    Kind   uint16
    Y      float64
    Alive  bool
    Z      float64
    ID     uint32
}
```

![El orden de los campos decide el padding: ParticleBefore ocupa 56 bytes, los mismos campos ordenados por alineamiento ocupan 32](images/10-padding.webp)

El laboratorio incluye un pequeño programa que imprime el layout usando `reflect` (que da los mismos offsets que `unsafe.Offsetof`):

```bash
cd labs
go run ./10-struct-layout/cmd/sizes
```

```
ParticleBefore: size=56 align=8 (heap size class 64)
    0  Active   bool     size=1 align=1
    1  padding  7 bytes
    8  X        float64  size=8 align=8
   16  Kind     uint16   size=2 align=2
   18  padding  6 bytes
   24  Y        float64  size=8 align=8
   32  Alive    bool     size=1 align=1
   33  padding  7 bytes
   40  Z        float64  size=8 align=8
   48  ID       uint32   size=4 align=4
   52  padding  4 bytes
```

Los campos suman 1+8+2+8+1+8+4 = **32 bytes**. El struct ocupa **56**: 24 bytes, el 43%, son padding. Ordena los campos de mayor a menor alineamiento y el padding desaparece:

```
ParticleAfter: size=32 align=8 (heap size class 32)
    0  X        float64  size=8 align=8
    8  Y        float64  size=8 align=8
   16  Z        float64  size=8 align=8
   24  ID       uint32   size=4 align=4
   28  Kind     uint16   size=2 align=2
   30  Active   bool     size=1 align=1
   31  Alive    bool     size=1 align=1
```

> **¿Por qué Go no reordena los campos por mí?** Porque el orden de los campos es observable: afecta a `unsafe.Offsetof`, a `reflect`, a los layouts binarios compartidos con C mediante cgo o `syscall`, y a la memoria que obtienes al convertir entre tipos struct. La especificación no promete nada sobre el layout, pero el compilador gc siempre ha respetado el orden de declaración, y mucho código depende de ello.

### La trampa del campo de tamaño cero al final

Un campo de tamaño cero (`struct{}`, `[0]int`) normalmente no cuesta nada. **Excepto al final de un struct.** De `CalcStructSize`:

```go
// For non-zero-sized structs which end in a zero-sized field, we
// add an extra byte of padding to the type. This padding ensures
// that taking the address of a zero-sized field can't manufacture a
// pointer to the next object in the heap. See issue 9401.
if size > 0 && fields[len(fields)-1].Type.width == 0 {
    size++
}
```

Un byte extra, luego el redondeo al alineamiento del struct, y un `int64` más un campo marcador duplica su tamaño. El programa `sizes` lo muestra:

```
Trailing: size=16 align=8 (heap size class 16)
    0  N        int64    size=8 align=8
    8  End      struct {} size=0 align=1
    8  padding  8 bytes

Leading: size=8 align=8 (heap size class 8)
    0  Start    struct {} size=0 align=1
    0  N        int64    size=8 align=8
```

Te lo encontrarás con marcadores `_ struct{}` de tipo "no copiar" o "no comparar", y con campos `[0]func()`. Ponlos al principio.

### Clases de tamaño: el allocator también redondea

Cuando reservas un objeto pequeño en el heap, `mallocgc` no te da exactamente `Sizeof` bytes. Los objetos de hasta 32 KB salen de **clases de tamaño**: spans de memoria divididos en huecos del mismo tamaño. La tabla se genera en `internal/runtime/gc/sizeclasses.go`:

```
// class  bytes/obj  bytes/span  objects  tail waste  max waste  min align
//     1          8        8192     1024           0     87.50%          8
//     2         16        8192      512           0     43.75%         16
//     3         24        8192      341           8     29.24%          8
//     4         32        8192      256           0     21.88%         32
//     5         48        8192      170          32     31.52%         16
//     6         64        8192      128           0     23.44%         64
//     7         80        8192      102          32     19.07%         16
```

El camino noscan de `runtime/malloc.go` (`mallocgcSmallNoscan`) busca la clase y sustituye el tamaño pedido por el tamaño de la clase:

```go
sizeclass = gc.SizeToSizeClass8[divRoundUp(size, gc.SmallSizeDiv)]
...
size = uintptr(gc.SizeClassToSize[sizeclass])
spc := makeSpanClass(sizeclass, true)
```

Así que un objeto de 33 bytes cuesta 48, y nuestra partícula de 56 bytes cuesta **64**. La partícula reordenada de 32 bytes cae justo en una clase. A veces ahorrar un solo byte te baja de clase. A veces ahorrar 20 bytes no cambia nada porque sigues en la misma clase. `fieldalignment` te da ambos números.

> **Novedad de Go 1.27: la búsqueda pasa a tiempo de compilación.** Cuando el tamaño se conoce al compilar y es menor de 80 bytes, el compilador ahora llama a una rutina especializada por tamaño de `runtime/malloc_generated.go`, con la clase fijada como constante. Ejecuta `go build -gcflags=-S ./10-struct-layout/` y mira `SpawnBefore`: llama a `runtime.mallocgcSmallNoScanSC6` (clase 6, 64 bytes); cuando hayas reordenado los campos (Paso 3), `SpawnAfter` llama a `runtime.mallocgcSmallNoScanSC4` (clase 4, 32 bytes). La asignación es más barata, pero el redondeo es exactamente el mismo: la clase se sigue eligiendo de la misma tabla.

### Una nota sobre punteros y el GC

¿Te has fijado en `makeSpanClass(sizeclass, true)` arriba? Los spans no solo se separan por tamaño, sino también por **noscan**: si el tipo contiene punteros o no. Un objeto sin punteros (como nuestra partícula: solo números y bools) va a un span noscan, y el GC nunca tiene que mirar dentro. Añadir un solo campo `string` o `*T` lo cambia. Volveremos a esto en [Ayudando al Garbage Collector](12-gc-pressure.md).

## El Laboratorio

```
labs/10-struct-layout/
  before.go      ParticleBefore y sus funciones (línea base, no editar)
  after.go       tu copia: haz ParticleAfter más pequeño
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
  cmd/sizes/     imprime el layout de ambos structs
```

Hay dos benchmarks, y cada uno mide una consecuencia del tamaño del struct:

| Benchmark | Qué hace | Qué cambia con un struct más pequeño |
|---|---|---|
| `Spawn` | reserva 10.000 partículas, un `&Particle{}` cada una | bytes por asignación (clase de tamaño) |
| `SumActive` | suma X+Y+Z sobre un `[]Particle` de 1.000.000 de elementos | bytes que la CPU lee de memoria |

### Paso 1: Mide la línea base

Desde la raíz del repositorio:

```bash
make bench LAB=10
```

`after.go` empieza siendo una copia de `before.go`, así que ambas columnas deberían ser iguales (`~`).

### Paso 2: Encuentra el desperdicio

Imprime el layout y comprueba el tamaño:

```bash
cd labs
go run ./10-struct-layout/cmd/sizes
```

Después deja que una herramienta lo encuentre por ti. `fieldalignment` está en `golang.org/x/tools` y se puede ejecutar directamente desde la caché de módulos:

```bash
go run golang.org/x/tools/go/analysis/passes/fieldalignment/cmd/fieldalignment@latest ./10-struct-layout/
```

```
.../10-struct-layout/after.go:14:20: ParticleAfter has size 56 (allocator size class 64) but the optimal size is 32 leading to a waste of 32 bytes (50%)
.../10-struct-layout/before.go:11:21: ParticleBefore has size 56 (allocator size class 64) but the optimal size is 32 leading to a waste of 32 bytes (50%)
```

Fíjate en que compara la **clase de tamaño** (64) con el tamaño óptimo (32): se desperdicia la mitad de cada asignación. También tiene un flag `-fix` que reescribe el struct por ti, pero lee antes la advertencia de su documentación: elimina los comentarios de los campos, y además optimiza los "pointer bytes" para el GC, lo que puede darte un orden que no esperas.

> `fieldalignment` no forma parte de `go vet` porque un struct más pequeño no siempre es mejor (ver "Entendiendo los Resultados"). Trátalo como un informe, no como un linter al que hay que silenciar.

### Paso 3: Arréglalo

Edita `labs/10-struct-layout/after.go` y reordena los campos de `ParticleAfter`: primero los tres `float64`, luego `uint32`, luego `uint16`, y luego los dos `bool`. No toques las funciones: los literales del struct usan nombres de campo, así que siguen compilando. Ejecuta otra vez `go run ./10-struct-layout/cmd/sizes` y comprueba que te sale 32.

### Paso 4: Compara

```bash
make bench LAB=10
```

Estos son los resultados con la solución de referencia (`make solution LAB=10 COUNT=8`) en un AMD Ryzen AI MAX+ 395:

```
             │    before     │                after                │
             │    sec/op     │    sec/op     vs base               │
Spawn-32        161.6µ ± 22%   111.4µ ± 13%  -31.08% (p=0.000 n=8)
SumActive-32   1166.8µ ±  2%   646.1µ ±  3%  -44.63% (p=0.000 n=8)

             │     before     │                 after                 │
             │      B/op      │     B/op      vs base                 │
Spawn-32       705.0Ki ± 0%     392.5Ki ± 0%  -44.33% (p=0.000 n=8)
SumActive-32     0.000 ± 0%       0.000 ± 0%        ~ (p=1.000 n=8)

             │    before     │                after                │
             │   allocs/op   │  allocs/op   vs base                │
Spawn-32       10.00k ± 0%     10.00k ± 0%       ~ (p=1.000 n=8)
```

## Entendiendo los Resultados

**Spawn** hace exactamente el mismo número de asignaciones, pero cada una ha pasado de la clase de 64 bytes a la de 32. La aritmética es exacta: 10.000 × 64 B + 81.920 B del slice `[]*Particle` = 705,0 KiB, y 10.000 × 32 B + 81.920 B = 392,5 KiB. La mejora en tiempo es menor y más ruidosa (reservar un objeto noscan pequeño es barato en ambos casos, y aún más desde la asignación especializada por tamaño de Go 1.27), pero la mitad de bytes también significa la mitad de crecimiento del heap, así que el GC se ejecuta la mitad de veces. Ahí está la ganancia a largo plazo.

**SumActive** es la más sorprendente: el código no ha cambiado en absoluto, es un **45% más rápido**, y no reserva nada. El bucle está limitado por la velocidad a la que la memoria le entrega datos, no por las sumas. Antes, el slice ocupaba 56 MB; ahora, 32 MB. Ambos son más grandes que los 32 MB de L3 de esta máquina, así que casi todos los datos vienen de RAM, y el tiempo es más o menos proporcional a los bytes leídos: de 56 a 32 MB es una reducción del 43%, y hemos medido un 45%. Con cache lines de 64 bytes, antes cabían 1,14 partículas por línea y ahora caben exactamente 2, así que ninguna partícula queda partida entre dos líneas. Verás exactamente por qué en la Parte IV, en [Cachés de CPU y Localidad de Datos](15-cpu-caches.md).

> **Más pequeño no siempre es mejor.** Reordenar puede separar campos que se usan juntos, y con escritores concurrentes, meter dos campos calientes en la misma cache line puede hacerlo todo *más lento* (false sharing, ver [Contención y False Sharing](16-contention-false-sharing.md)). Por defecto, ordena los campos por tamaño, y por patrón de acceso cuando un perfil te lo pida.

## Puntos Clave

- Go mantiene los campos en el orden de declaración y añade padding a cada uno según su alineamiento
- Ordenar los campos de mayor a menor alineamiento elimina casi todo el padding
- Un campo de tamaño cero **al final** de un struct añade padding: pon los marcadores al principio
- Los objetos del heap se redondean a una **clase de tamaño**: 56 bytes cuestan 64, 33 bytes cuestan 48
- `unsafe.Sizeof`, volcados de layout como `cmd/sizes` y `fieldalignment` te muestran el desperdicio
- Un elemento más pequeño significa más elementos por cache line, así que los bucles sobre slices grandes se aceleran gratis
- Los structs sin punteros van a spans noscan que el GC nunca recorre

## Para Ir Más Allá (opcional)

1. Añade un campo `Name string` a `ParticleAfter`. ¿Dónde debería ir? ¿Qué pasa con el tamaño y la clase de tamaño? ¿Sigue siendo noscan el tipo?
2. Cambia `sumN` en `lab_test.go` a `10_000` para que todo el slice quepa en L2 (1 MB). ¿Cómo cambia la mejora de `SumActive`? ¿Por qué?
3. Añade `_ struct{}` como último campo de `ParticleAfter` y ejecuta `cmd/sizes`. Después muévelo al principio.
4. Ejecuta `fieldalignment` sobre un proyecto en el que trabajes. ¿Cuántos structs reporta, y se reserva alguno de ellos millones de veces?
