# Interfaces y Boxing

Las interfaces hacen que el código sea flexible, pero pueden añadir asignaciones y ocultar al compilador oportunidades de optimización. Exploraremos ambos costes con dos ejercicios pequeños sobre precios de productos.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Entender el boxing y cuándo puede reservar memoria
- Reconocer el coste de las llamadas a interfaces en un bucle frecuente
- Comparar llamadas a métodos mediante interfaces y tipos concretos
- Evitar el boxing innecesario al guardar una lista de precios

## El Concepto: Qué Guarda una Interfaz

### Qué contiene una interfaz

`any` puede guardar valores de distintos tipos. Por ejemplo:

```go
price := 1000
var value any = price
```

Go recuerda dos cosas: **el tipo** (`int`) y **dónde encontrar el valor** (`1000`).

Para un valor como este número, Go puede guardar una copia separada y hacer que la interfaz apunte a ella. Esto se llama **boxing**. La copia puede necesitar memoria extra en el heap, pero el compilador puede evitar esa asignación en algunos casos. Usar `any` no siempre asigna memoria.

![Un valor any recuerda el tipo int y apunta a una copia del precio 1000; el boxing puede necesitar memoria extra en el heap](images/08-iface.es.webp)

### Llamar a un método a través de una interfaz

`any` no exige ningún método. Otras interfaces describen qué puede **hacer** un valor. Aquí, `PriceFilter` acepta cualquier tipo con un método `Keep`, y `*Budget` es uno de ellos:

```go
type PriceFilter interface {
    Keep(price int) bool
}

type Budget struct { MaxCents int }

func (b *Budget) Keep(price int) bool {
    return price <= b.MaxCents
}
```

Podemos guardar un presupuesto en la interfaz y llamar a su método:

```go
var filter PriceFilter = &Budget{MaxCents: 1500}
affordable := filter.Keep(1000) // true
```

Go usa el tipo concreto guardado en `filter` para encontrar el método `Keep` correcto. En este caso llama al método de `Budget` con el presupuesto guardado.

### Por qué importan las llamadas a métodos

Cuando una función recibe `PriceFilter`, podría recibir distintos tipos de filtro. El compilador puede no saber qué método `Keep` se ejecutará, así que no puede hacer inline de esa llamada.

Si recibe `*Budget`, conoce el método y puede integrar su comparación mediante inlining. Reconocer una implementación concreta detrás de una interfaz se llama **desvirtualización**.

## El Laboratorio

Los dos ejercicios usan precios de productos en céntimos. Primero veremos las **asignaciones** y después el **tiempo de ejecución**. Edita `labs/08-interfaces-boxing/after.go` y ejecuta los comandos desde la raíz del repositorio.

### Ejercicio 1: Guarda precios sin boxing

`CopyPricesAfter` copia 1.000 precios a un `[]any`. Pero todos son `int`, así que esa flexibilidad extra no hace falta.

**Mide:**

```bash
make bench LAB=08 BENCH=CopyPrices
```

**Cambia:** sustituye `[]any` por `[]int` en el tipo devuelto y en `make`. Mantén el bucle como está:

```go
func CopyPricesAfter(prices []int) []int {
    out := make([]int, len(prices))
    for i, price := range prices {
        out[i] = price
    }
    return out
}
```

**Comprueba y compara:**

```bash
(cd labs && go test ./08-interfaces-boxing/)
make bench LAB=08 BENCH=CopyPrices
```

Con la solución de referencia, en mi máquina:

| Asignaciones por copia | Antes | Después |
|---|---|---|
| 1.000 precios | 1.001 | 1 |

La asignación restante es el slice de salida. Hemos quitado la copia empaquetada de cada precio. El benchmark usa precios mayores que 255; Go puede usar almacenamiento compartido para enteros pequeños empaquetados.

### Ejercicio 2: Haz directa la comprobación del presupuesto

`CountAffordableAfter` cuenta precios que caben en un presupuesto. Su bucle llama a `budget.Keep(price)` por cada uno de los 10.000 precios.

**Mide:**

```bash
make bench LAB=08 BENCH=CountAffordable
```

Las dos versiones asignan cero memoria. Esta vez fíjate en **`sec/op`**.

**Cambia:** sustituye el tipo del parámetro `PriceFilter` por `*Budget`. Mantén el cuerpo como está:

```go
func CountAffordableAfter(prices []int, budget *Budget) int
```

Mantén el `//go:noinline` de esta función exterior. La comparación que queremos integrar está en `Budget.Keep`, **dentro** de su bucle.

**Comprueba y compara:**

```bash
(cd labs && go test ./08-interfaces-boxing/)
make bench LAB=08 BENCH=CountAffordable
```

En mi máquina, la solución tarda unos **2,8 µs**, frente a **8–12 µs** de la versión con interfaz. Las dos siguen indicando cero asignaciones.

## Entendiendo los Resultados

El primer cambio evita el boxing porque el slice guarda enteros directamente. El segundo permite integrar un método diminuto porque se conoce su tipo. Ninguno cambia qué precios se copian o se cuentan.

Las interfaces siguen siendo útiles si necesitas distintas implementaciones. Usa un tipo concreto donde no haga falta esa flexibilidad y las mediciones indiquen una mejora.

## Puntos Clave

- Si todos los valores son enteros, `[]int` puede evitar el boxing de `[]any`.
- Una implementación de método conocida puede hacerse inline.
- Mide las asignaciones y el tiempo de ejecución por separado.

## Para Ir Más Allá (opcional)

1. Cambia los precios de prueba a valores menores que 256. ¿Qué pasa con las asignaciones del boxing?
2. Cambia el límite del presupuesto. ¿Cuentan las dos versiones el mismo número de productos asequibles?
