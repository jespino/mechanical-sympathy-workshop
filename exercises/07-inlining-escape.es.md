# Inlining × Escape Analysis

> **¿Quieres saber más?** [El IR](https://internals-for-interns.com/es/posts/the-go-ir/#el-pipeline-de-optimización), en Internals for Interns, describe el orden de los pases de optimización del compilador, que es el motivo por el que el inlining puede cambiar lo que escapa.

El inlining puede hacer más que eliminar una llamada. También puede ayudar al compilador a mantener un objeto en el stack, aunque un constructor devuelva un puntero. Este módulo combina las ideas de los dos anteriores.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Explicar por qué el inlining puede evitar una asignación en el heap
- Separar un constructor en una función pequeña y un método de inicialización
- Comprobar el resultado con la salida del compilador y `allocs/op`

## El Concepto: Las Asignaciones Se Mueven Con el Inlining

### Por qué importa el orden

El compilador hace **inlining antes del escape analysis**. Si copia un constructor dentro de quien lo llama, después puede ver cómo se usa el objeto nuevo.

Si el objeto solo se usa localmente, puede quedarse en el stack de quien llama. Si se guarda en un sitio que vive más tiempo, sigue necesitando el heap. Devolver un puntero no lo explica todo: también importa quién llama.

### Mantén pequeño el constructor

Mueve las comprobaciones y la preparación a un método `init`, dejando un constructor pequeño:

```go
func NewCustomer(input Signup) (*Customer, error) {
    c := &Customer{}
    if err := c.init(input); err != nil {
        return nil, err
    }
    return c, nil
}
```

El constructor puede hacerse inline aunque `init` sea demasiado grande. Lo importante es que `init` no guarde `c` en un sitio que viva más que la llamada. Añadir trabajo al constructor puede hacer que supere el presupuesto de inlining.

## El Laboratorio

Estás procesando formularios de alta de clientes. Cada uno tiene un nombre y un email. `NewCustomerBefore` quita espacios, comprueba que haya nombre y que el email tenga una sola `@` con texto a ambos lados, y devuelve un `*CustomerBefore`.

`CountValidBefore` cuenta los formularios válidos. Solo necesita cada cliente durante el bucle, pero el constructor es demasiado grande para hacerse inline: 1.000 formularios válidos provocan 1.000 asignaciones en el heap.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=07
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go build -gcflags=-m=2 ./07-inlining-escape/ 2>&1 | grep -E 'inline NewCustomerBefore|CustomerBefore.*escapes')
```

Busca `cannot inline NewCustomerBefore` y `escapes to heap`. El constructor tiene un coste de 261, por encima del presupuesto habitual de 80.

### Paso 3: Arréglalo

Edita `labs/07-inlining-escape/after.go`. Mantén `before.go` como referencia.

1. Mueve el trabajo de `NewCustomerAfter` a `func (c *CustomerAfter) init(input Signup) error`.
2. En `init`, devuelve los errores directamente. Si todo va bien, rellena `*c` en lugar de devolver un cliente nuevo y devuelve `nil`.
3. Usa el constructor pequeño de arriba con el tipo `CustomerAfter`. Deja `CountValidAfter` como está.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./07-inlining-escape/)
make bench LAB=07
```

Solución de referencia (`make solution LAB=07`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| Tiempo de `CountValid` | unos 17 µs | unos 7,8 µs |
| Asignaciones por ejecución | 1.000 | 0 |

## Entendiendo los Resultados

Desaparecen las 1.000 asignaciones de clientes y este benchmark es aproximadamente el doble de rápido. Las comprobaciones de nombre y email no cambian; el cliente temporal vive ahora en el stack de quien llama.

Vuelve a usar `-gcflags=-m=2` y busca `can inline NewCustomerAfter` y `does not escape` en la llamada de `CountValidAfter`. El constructor por separado puede seguir mostrando `escapes to heap`: la copia integrada y la función independiente se analizan por separado.

Esto solo funciona mientras el constructor quepa en el presupuesto y quien llama use el cliente localmente. Un puntero guardado para usarlo después puede seguir escapando.

## Puntos Clave

- El inlining permite ver al escape analysis cómo usa el objeto quien llama.
- Mantén pequeños los constructores y mueve la preparación a un método que no guarde el receptor.
- Devolver un puntero puede evitar el heap si la llamada se hace inline.
- Confírmalo con la salida del compilador y `allocs/op`.

## Para Ir Más Allá (opcional)

1. Añade una comprobación al constructor pequeño. ¿Sigue haciéndose inline?
2. Guarda los clientes en un slice que se conserve después de que termine `CountValidAfter`. ¿Vuelven las asignaciones?
