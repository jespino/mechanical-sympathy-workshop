# Eliminación de Bounds Checks

> **¿Quieres saber más?** Las comprobaciones de límites se eliminan en la etapa SSA del compilador. [La Fase SSA](https://internals-for-interns.com/es/posts/the-go-ssa/), en Internals for Interns, presenta esa etapa y enseña a observar cada pase con `GOSSAFUNC`. El [capítulo sobre los pases de optimización SSA](https://www.youtube.com/watch?v=qnmoAA0WRgE&t=1726s) de la charla *Understanding the Go Compiler* de Jesús Espino en GopherCon UK (en inglés) da la visión general.

Go comprueba los índices de los slices para evitar lecturas y escrituras fuera de sus límites. El compilador puede quitar una comprobación si demuestra que el acceso es seguro. En este módulo le ayudamos a ver esa seguridad sin quitarla nosotros.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Entender para qué sirven las comprobaciones de límites
- Listar las comprobaciones que deja el compilador
- Escribir bucles cuyos índices sean fáciles de demostrar seguros
- Mantener el comportamiento cuando los slices son demasiado cortos

## El Concepto: Comprobaciones, y Cómo Demostrar que Sobran

### Cuándo sobra una comprobación

En `for i := range s { use(s[i]) }`, el bucle ya garantiza que el índice es válido. El compilador puede quitar la comprobación extra de `s[i]`. Usa las longitudes y comparaciones conocidas para encontrar estas garantías.

### Haz evidente que el acceso es seguro

| Patrón | Qué sabe el compilador |
|---|---|
| `for i := range s` | `i` está dentro de `s` |
| ajustar las entradas a la misma longitud | un índice válido en un slice puede ser válido en los demás |
| comprobar las longitudes antes del bucle | se rechaza una entrada corta antes de procesarla |

Una comprobación suele ser barata. Quitar las repetidas puede ayudar en bucles que hacen muy poco trabajo más. Úsalo cuando un benchmark o un perfil señale un bucle muy usado.

### Longitud y capacidad son distintas

`len(s)` es el número de elementos actuales del slice. `cap(s)` indica hasta dónde se puede ampliar. `s[:n]` puede llegar hasta la capacidad, así que **no** demuestra que `n <= len(s)`. Comprueba las longitudes si la función debe rechazar una entrada corta.

## El Laboratorio

Estás imprimiendo un ticket de compra. En cada línea, multiplicas el precio del producto por su cantidad y restas el descuento de la línea. Los precios y descuentos están en céntimos.

`LineTotalsBefore(dst, prices, quantities, discounts, n)` rellena los primeros `n` valores de salida. Por ejemplo, dos productos de 100 céntimos con un descuento de 10 producen un total de 190 céntimos.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=09
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go build -gcflags='-d=ssa/check_bce/debug=1' ./09-bounds-check-elimination/ 2>&1 | grep before.go)
```

Cada línea `Found IsInBounds` o `Found IsSliceInBounds` es una comprobación que queda. La versión inicial tiene cuatro en `LineTotalsBefore`: una por cada acceso a un slice.

### Paso 3: Arréglalo

Edita `labs/09-bounds-check-elimination/after.go`. Mantén `before.go` como referencia.

1. Devuelve directamente si `n <= 0`, como hace el bucle vacío de la versión inicial.
2. Comprueba que los cuatro slices tengan al menos `n` elementos. Mantén el panic si alguno es corto, aunque tenga capacidad de sobra.
3. Ajusta los slices a longitud `n` y recorre con `range dst`:

```go
if n > len(dst) || n > len(prices) || n > len(quantities) || n > len(discounts) {
    panic("LineTotalsAfter: slice shorter than n")
}
dst, prices, quantities, discounts = dst[:n], prices[:n], quantities[:n], discounts[:n]
for i := range dst {
    dst[i] = prices[i]*quantities[i] - discounts[i]
}
```

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./09-bounds-check-elimination/)
make bench LAB=09
```

Solución de referencia (`make solution LAB=09`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| `LineTotals` | unos 1,9 µs | unos 1,3 µs |

## Entendiendo los Resultados

Los cálculos del ticket no cambian, pero el benchmark tarda alrededor de un 30% menos. El compilador puede demostrar que los índices son válidos sin repetir cuatro comprobaciones por línea.

Vuelve a listar las comprobaciones usando `grep after.go`. Si no sale nada, no quedan comprobaciones en ese archivo. Las comprobaciones fuera del bucle están bien: el objetivo es evitar repetir el mismo trabajo por cada elemento. En funciones con más trabajo puede haber poca mejora, así que mide siempre.

## Puntos Clave

- Mantén la seguridad de los límites y ayuda al compilador a demostrarla.
- `range` y las comprobaciones claras de longitud facilitan razonar sobre los índices.
- Ajustar un slice comprueba la capacidad; valida las longitudes cuando haga falta.
- Usa los diagnósticos del compilador y mide el resultado.

## Para Ir Más Allá (opcional)

1. Sustituye `range dst` por `for i := 0; i < n; i++`. ¿Vuelven las comprobaciones?
2. Dale a `prices` capacidad de sobra, pero una longitud menor que `n`. ¿Por qué debe seguir haciendo panic?
3. Añade más trabajo por línea del ticket. ¿Sigue notándose tanto quitar las comprobaciones?
