# Layout de Structs y Clases de Tamaño

> **¿Quieres saber más?** [El Memory Allocator](https://internals-for-interns.com/es/posts/go-memory-allocator/), en Internals for Interns, explica en detalle las arenas, los spans y las size classes.

El orden de los campos de un struct puede cambiar cuánta memoria ocupa. Go puede insertar bytes sin usar entre campos para mantenerlos alineados. Reordenaremos un struct y mediremos el efecto en las asignaciones y en los bucles que recorren sus datos.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Reconocer el relleno entre campos de un struct
- Medir el tamaño del struct y la posición de sus campos
- Entender por qué las asignaciones del heap se redondean a clases de tamaño
- Reducir el uso de memoria reordenando campos

## El Concepto: Padding, Alineamiento y Clases de Tamaño

### Alineación y relleno

Algunos valores deben empezar en una dirección múltiplo de su alineación. Por ejemplo, un `int64` suele necesitar alineación de 8 bytes. Si justo antes hay un `bool`, Go puede insertar siete bytes sin usar para llegar a la siguiente dirección adecuada. Esos bytes son el **relleno** o padding.

Go mantiene los campos en el orden en que los declaras. Agrupar primero los de mayor alineación suele reducir el relleno.

El pedido del laboratorio tiene 32 bytes de campos, pero ocupa 56 bytes con el orden original. Reordenarlos reduce el tamaño a 32 bytes sin quitar datos.

### Clases de tamaño

El asignador del heap usa huecos de tamaño fijo llamados **clases de tamaño**. Un pedido individual de 56 bytes ocupa un hueco de 64; uno de 32 cabe en uno de 32. Reducir el struct puede ahorrar todavía más espacio en el heap.

![Reordenar los mismos campos elimina 24 bytes de relleno: el struct pasa de 56 a 32 bytes y su bloque individual del heap pasa de 64 a 32 bytes](images/10-padding.es.webp)

Las zonas sombreadas son relleno. Los campos siguen guardando exactamente los mismos datos.

### Huecos dentro de un span

Go gestiona la memoria de los objetos pequeños en bloques mayores llamados **spans**. Cada span se divide en huecos iguales y sirve a una clase de tamaño.

En este ejemplo, los dos spans tienen 8 KiB: uno contiene **256 huecos de 32 bytes** y el otro **128 huecos de 64 bytes**. Los objetos más pequeños permiten aprovechar la misma memoria para más objetos.

![Dos spans de 8 KiB contienen 256 huecos de 32 bytes o 128 huecos de 64 bytes; un pedido de 56 bytes usa un hueco de 64 y deja 8 bytes sin usar](images/10-spans.es.webp)

Un pedido de 56 bytes ocupa un hueco de 64 y deja 8 bytes sin usar. Este redondeo es distinto del relleno dentro del struct, que ya está incluido en esos 56 bytes.

Un slice de structs es distinto: sus elementos se colocan seguidos usando el tamaño del struct. Un millón de pedidos de 56 bytes necesita unos 56 MB; si son de 32 bytes, unos 32 MB.

### Una pequeña sorpresa

Incluso un campo de tamaño cero como `struct{}` puede añadir relleno si es el último campo de un struct no vacío. Pon primero los campos de marcador y comprueba el tamaño.

`unsafe.Sizeof`, `unsafe.Alignof` y `unsafe.Offsetof` indican tamaños y posiciones. El programa `cmd/sizes` del laboratorio los muestra por ti.

## El Laboratorio

`OrderBefore` guarda un ID de pedido, número de productos, indicadores de pago y entrega, y tres importes en céntimos. `CreateOrders` reserva 10.000 pedidos. `TotalPaid` suma subtotal más envío menos descuento de los pedidos pagados en un slice de un millón de elementos. Solo cambiaremos el orden de los campos de `OrderAfter`.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=10
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go run ./10-struct-layout/cmd/sizes)
```

Busca `OrderBefore: size=56` y los huecos entre sus campos. Compara la suma del tamaño de los campos con el tamaño del struct.

### Paso 3: Arréglalo

Edita `labs/10-struct-layout/after.go`. Mantén `before.go` como referencia.

1. Pon primero los tres campos `int64`, después `uint32`, `uint16` y los dos `bool`, como se muestra abajo.
2. Mantén las funciones como están; sus literales de struct usan nombres de campo.
3. Vuelve a ejecutar `cmd/sizes` y comprueba que `OrderAfter` ocupa 32 bytes.

```go
type OrderAfter struct {
    SubtotalCents int64
    ShippingCents int64
    DiscountCents int64
    ID            uint32
    Items         uint16
    Paid          bool
    Delivered     bool
}
```

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./10-struct-layout/)
make bench LAB=10
```

Solución de referencia (`make solution LAB=10`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| Tiempo de `CreateOrders` | unos 202 µs | unos 131 µs |
| Bytes por ejecución de `CreateOrders` | 705,0 KiB | 392,5 KiB |
| Tiempo de `TotalPaid` | unos 1,27 ms | unos 0,68 ms |

## Entendiendo los Resultados

**CreateOrders** sigue haciendo 10.000 asignaciones de pedidos, pero cada una es menor. Fíjate en `B/op`: menos bytes importan aunque `allocs/op` no cambie.

**TotalPaid** tarda aproximadamente la mitad sin cambiar el bucle. La CPU lee menos memoria para el mismo número de pedidos. Lo veremos en [Cachés de CPU](16-cpu-caches.md).

Un struct más pequeño no siempre es más rápido. Los campos usados juntos suelen beneficiarse de estar cerca; los escritos por distintas goroutines pueden interferir entre sí. Mide el patrón de acceso además del tamaño.

## Puntos Clave

- El orden de los campos afecta al relleno y al tamaño del struct.
- Agrupa primero los campos de mayor alineación y comprueba la distribución.
- Las clases de tamaño del heap pueden aumentar el ahorro.
- Los registros más pequeños pueden acelerar recorridos grandes.

## Para Ir Más Allá (opcional)

1. Añade un campo `CustomerName string`. ¿Dónde puedes colocarlo para minimizar el relleno?
2. Pon `_ struct{}` al final y después al principio. Compara los tamaños.
3. Reduce `sumN` a `10_000`. ¿Cambia la mejora con menos datos?
