# Inlining

En [Escape Analysis](05-escape-analysis.md) vimos una línea de la salida de `-m` que decía `inlining call to normalizeBefore`. El inlining consiste en que el compilador copia el cuerpo de una función dentro de la función que la llama, en lugar de emitir una llamada. Es la optimización que hace posibles muchas otras, y Go es bastante conservador con ella. En este módulo veremos las reglas que usa el compilador para decidir, y haremos que el caso común de una función muy usada sea lo bastante pequeño para hacerse inline.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás cuánto cuesta una llamada a función, y qué gana el inlining además de eliminarla
- Entenderás el **presupuesto** de inlining (80) y cómo comprobar el coste de una función
- Sabrás qué impide el inlining
- Sabrás leer en `-gcflags=-m=2` los mensajes "can inline ... with cost N" y "cannot inline ... cost N exceeds budget 80"
- Aplicarás la **separación fast path / slow path** para conseguir que una función caliente se haga inline

## El Concepto: Lo Que Aporta el Inlining

Llamar a una función requiere un pequeño trabajo extra además de ejecutar su código. Normalmente no importa, pero se acumula cuando una función pequeña se llama miles de veces en un bucle.

El **inlining** sustituye la llamada por el código de la función. Así se ahorra el coste de la llamada y el compilador puede optimizar ambas partes juntas, por ejemplo eliminando comprobaciones que ahora puede demostrar que sobran. En el [siguiente módulo](07-inlining-escape.md) veremos cómo esto también puede evitar asignaciones en el heap.

La contrapartida es más código: hacer inline de una función en muchos sitios copia su cuerpo en cada uno. Para limitar ese crecimiento, el compilador normalmente solo hace inline de funciones pequeñas.

### Un ejemplo rápido: sumar dos números

Estas funciones hacen la misma suma. La directiva `//go:noinline` obliga a mantener la segunda como llamada a función:

```go
func add(a, b int) int { return a + b }

//go:noinline
func addNoInline(a, b int) int { return a + b }
```

Si medimos las dos en un bucle muy ajustado, la llamada cuesta unos **0,6 ns** más que la versión con inlining en mi máquina (unos 0,83 ns frente a 0,20 ns por suma). Eso incluye la propia llamada y las optimizaciones que impide. Es poco, pero se acumula si una función hace muy poco trabajo y se llama a menudo. En el laboratorio veremos cómo se acumula en código realista.

### El presupuesto

El compilador asigna a cada función una **puntuación de coste** según el código que contiene. El presupuesto habitual de inlining es **80**: una función que cabe en él puede hacerse inline, salvo que algo más lo impida.

Esta puntuación mide la complejidad del código, **no el tiempo de ejecución ni las líneas de código**. Llamar a una función que no puede hacerse inline añade **57 puntos**, así que incluso una función corta puede superar el presupuesto.

No necesitas calcular la puntuación a mano. Usa `-gcflags=-m=2` para preguntarle al compilador. En el laboratorio, separaremos una función de coste **161** en una función pequeña de coste **70** y un helper para el trabajo restante.

### Qué impide el inlining

Una función pequeña puede quedarse sin inlining si contiene `defer`, `go` o `recover`. Añadir `//go:noinline` también le indica al compilador que mantenga la llamada a función.

Los bucles, los closures y `select` no impiden el inlining por sí solos. En caso de duda, pregúntale al compilador.

### Leyendo la respuesta del compilador

Usa `go build -gcflags=-m=2` para ver las decisiones del compilador. Busca tres mensajes:

- `can inline F`: la función puede hacerse inline.
- `inlining call to F`: esta llamada concreta se ha sustituido por el código de la función.
- `cannot inline F`: la función no puede hacerse inline; el mensaje explica por qué, por ejemplo por superar el presupuesto o contener `defer`.

El flag más corto, `-gcflags=-m`, muestra qué funciones pueden hacerse inline y en qué llamadas se ha hecho. Usa `-m=2` para ver los costes y los motivos.

### PGO

Un perfil de CPU muestra al compilador qué código se ejecuta más a menudo. Con la **optimización guiada por perfiles (PGO)**, puede dar a esas llamadas frecuentes un presupuesto de inlining mayor, de hasta **2000**. Lo probaremos en el [módulo 15](15-pgo.md).

## El Laboratorio

Estás calculando el total de un carrito de compra. Cada producto tiene un precio en céntimos y un cupón opcional:

```go
type Item struct {
    PriceCents int
    Coupon     string
}
```

`TotalBefore` llama a `priceBefore` para cada uno de los 10.000 productos. La mayoría no tiene cupón: su precio final es simplemente `max(cents, 0)`. Con cupón hay más trabajo: quitar espacios, convertir el código a mayúsculas y aplicar `SAVE10`, `SAVE20` o `HALF`. Los códigos desconocidos dejan el precio como está.

La función ya comprueba primero si el cupón está vacío. Pero las otras ramas la hacen demasiado grande para hacerse inline, así que incluso el caso sencillo paga una llamada a función.

### Paso 1: Mide la línea base

Ejecuta estos comandos desde la raíz del repositorio:

```bash
make bench LAB=06
```

### Paso 2: Pregúntale al compilador

```bash
(cd labs && go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep 'cannot inline priceBefore')
```

Busca `cost 161 exceeds budget 80`. El coste incluye todas las ramas, aunque la mayoría de las llamadas no use el código de los cupones.

### Paso 3: Mueve los cupones a un helper

Edita `labs/06-inlining/after.go`:

1. Mueve todo lo que viene después de comprobar el cupón vacío a `func priceWithCoupon(cents int, coupon string) int`. Mantén las reglas de los cupones.
2. Deja `priceAfter` como la función pequeña de abajo.
3. No cambies `TotalAfter`.

```go
func priceAfter(cents int, coupon string) int {
    if coupon == "" {
        return max(cents, 0)
    }
    return priceWithCoupon(cents, coupon)
}
```

Ahora el compilador puede copiar el caso común directamente en el bucle del total. Solo los productos con cupón necesitan llamar al helper más grande.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./06-inlining/)
(cd labs && go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep -E 'can inline priceAfter|inlining call to priceAfter')
make bench LAB=06
```

Los tests cubren precios normales, cupones, espacios, redondeo y precios negativos. El compilador debería mostrar `can inline priceAfter with cost 70` e `inlining call to priceAfter`.

Con la solución de referencia (`make solution LAB=06`), en mi máquina:

| Medida | Antes | Después |
|---|---|---|
| Tiempo por 10.000 productos | unos 14 µs | unos 4,6 µs |
| Asignaciones por ejecución | 0 | 0 |

## Entendiendo los Resultados

La solución de referencia es unas **3 veces más rápida** con esta carga. Aplica las mismas reglas y conserva la comprobación del cupón vacío. El cambio hace que ese caso común sea lo bastante pequeño para integrarse en el bucle. Como comprobación, desactivar el inlining de `priceAfter` devuelve el tiempo a unos 14 µs.

El benchmark da cupones al 1% de los productos. Procesar un cupón sigue requiriendo una llamada normal; ese trabajo no desaparece. Convertir códigos en minúsculas a mayúsculas también puede asignar memoria: la comparación usa códigos en mayúsculas, pero los tests siguen cubriendo las minúsculas.

El inlining ayuda sobre todo en operaciones pequeñas repetidas muchas veces. Si todos los productos necesitan procesar cupones, evitar la llamada importa mucho menos.

## Puntos Clave

- El compilador considera toda la función al calcular el coste de inlining.
- Mueve el trabajo menos frecuente a un helper para mantener pequeño el caso común.
- Usa `-gcflags=-m=2` para comprobar que la llamada se ha hecho inline.
- Mide entradas realistas y conserva el comportamiento.

## Para Ir Más Allá (opcional)

1. Añade `//go:noinline` encima de `priceAfter` y vuelve a medir. ¿Cuánta mejora queda?
2. Cambia `testCart` para que la mitad de los productos tenga cupón y después todos. ¿Cómo cambia la ventaja?
3. Añade una regla de cupón a `priceWithCoupon`. ¿Sigue haciéndose inline de `priceAfter`?
