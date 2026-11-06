# Goroutines, Channels y el Scheduler

Las goroutines y los channels facilitan organizar trabajo concurrente, pero crear workers y enviar mensajes sigue costando tiempo. Si cada tarea es diminuta, la coordinación puede costar más que la propia tarea. Agruparemos el trabajo en bloques y lo compararemos con un bucle normal.

## Contenido que Quizá Quieras Ver Primero

- [El Scheduler](https://internals-for-interns.com/es/posts/go-runtime-scheduler/) (artículo, Internals for Interns): el modelo GMP y cómo llega a ejecutarse una goroutine.
- [Slices, Maps y Channels](https://internals-for-interns.com/es/posts/go-runtime-slices-maps-channels/#channels) (artículo, Internals for Interns): qué es un channel por dentro y cómo funcionan los envíos y las recepciones.
- [Understanding the Go Runtime: scheduler y system monitor](https://www.youtube.com/watch?v=arH3jp_x8yQ&t=1852s) (charla de Jesús Espino, GopherCon UK, Go 1.21, en inglés): el capítulo con una visión general de ambos.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Entender por qué la concurrencia puede ser más lenta con tareas pequeñas
- Usar una traza de ejecución para encontrar goroutines esperando
- Enviar lotes en lugar de elementos individuales
- Repartir el trabajo entre un número limitado de workers

## El Concepto: Lo Que Cuesta Coordinar

### Crear y planificar trabajo

`go f()` pide al runtime que prepare una goroutine, le dé espacio de stack y la planifique. Go reutiliza recursos cuando puede, pero estos pasos siguen teniendo un coste. Crear una goroutine por cada cálculo diminuto puede gastar más tiempo del que ahorra.

Puedes medir el coste de crear muchas goroutines bloqueadas con:

```bash
(cd labs && go run ./13-goroutines-channels/cmd/gocost)
```

### Enviar por un channel

Un envío puede copiar un valor a un buffer o pasarlo a un receptor que espera. Si la operación no puede continuar, la goroutine espera y después debe volver a planificarse. El uso concurrente del channel también necesita sincronización.

![Cada envío por un channel toma el lock del channel y puede despertar una goroutine; agrupar paga ese coste una vez por bloque en lugar de una vez por elemento](images/13-channel.webp)

Un channel con buffer puede reducir las esperas, pero no elimina el coste de enviar cada elemento. Enviar un slice de 1.024 elementos reparte esa coordinación entre todo el lote. Un subslice apunta a los datos originales; no copia sus elementos.

### Cuándo tiene sentido una goroutine por tarea

Una goroutine por petición o conexión suele ser un buen diseño: cada tarea hace trabajo relevante o espera por I/O. Para cálculos pequeños de CPU, empieza con un bucle normal y después prueba un número fijo de workers sobre bloques. Mide ambos.

## El Laboratorio

Estás aplicando una rebaja del 10%. Cada entrada es un precio en céntimos y el cálculo es `cents - cents/10` (redondeando el descuento hacia abajo). Los dos ejercicios procesan 100.000 precios. `TotalPricesBefore` suma los precios rebajados enviándolos uno a uno por un channel sin buffer. `DiscountPricesBefore` genera la lista de precios rebajados con una goroutine por elemento. El benchmark `TotalPrices` incluye `impl=sequential`, un bucle normal como referencia. Compara `ns/item`, el tiempo por elemento.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=13
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go test -run '^$' -bench 'TotalPrices/impl=before' -benchtime 20x -trace trace.out ./13-goroutines-channels/)
```

Desde `labs/`, abre `go tool trace trace.out`. Busca workers que esperan repetidamente en el channel de tareas. El productor dedica tanto tiempo a repartir elementos individuales que no consigue mantener ocupados a los workers.

### Paso 3: Arréglalo

Edita `labs/13-goroutines-channels/after.go`. Mantén `before.go` como referencia.

1. En `TotalPricesAfter`, cambia `jobs` a `chan []uint64` y envía bloques de hasta 1.024 elementos: `items[i:min(i+1024, len(items))]`. Dale al channel un buffer pequeño, por ejemplo `runtime.GOMAXPROCS(0)`.
2. Haz que cada worker procese todo su bloque antes de recibir otro. Mantén la forma actual de combinar las sumas parciales.
3. En `DiscountPricesAfter`, crea `runtime.GOMAXPROCS(0)` workers. Dale a cada uno un rango continuo y separado de índices de `out`, y espera a que terminen todos. Lee la entrada sin modificarla.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./13-goroutines-channels/)
make bench LAB=13
```

Solución de referencia (`make solution LAB=13`), en mi máquina:

| Benchmark | Antes | Después | Secuencial |
|---|---|---|---|
| Tiempo por elemento de `TotalPrices` | unos 180 ns | unos 0,84 ns | unos 0,38 ns |
| Tiempo por elemento de `DiscountPrices` | unos 265 ns | unos 0,92 ns | — |
| Asignaciones por ejecución de `DiscountPrices` | unas 200.000 | 66 | — |

## Entendiendo los Resultados

Las versiones concurrentes originales son más lentas que el bucle normal porque la coordinación pesa más que el cálculo pequeño. Añadir workers no arregla ese cuello de botella.

Los lotes hacen `TotalPrices` mucho más rápido que la versión que envía cada precio por separado. Dar a `DiscountPrices` un conjunto limitado de workers evita casi todas las asignaciones de goroutines por elemento. El slice de salida sigue necesitando memoria.

El bucle normal sigue ganando con estos cálculos de descuento diminutos. Es un resultado útil: agrupar trabajo arregla un problema de coordinación, pero no significa que el trabajo necesitase concurrencia desde el principio.

La mejora exacta depende de los cores disponibles y del tamaño del bloque. Uno demasiado grande puede dejar workers sin trabajo; uno demasiado pequeño vuelve a aumentar la coordinación.

## Puntos Clave

- Compara el código concurrente con una referencia secuencial.
- Agrupa tareas pequeñas para que cada operación de channel transporte trabajo útil.
- Usa un número limitado de workers para lotes de cálculo.
- Mantén separados los rangos de salida y espera antes de leer el resultado.

## Para Ir Más Allá (opcional)

1. Prueba bloques de 16, 64, 256 y 4096 elementos. ¿Cuál funciona mejor?
2. Añade un buffer al channel original de elementos individuales. ¿Cuánto ayuda frente a los lotes?
3. Ejecuta con `CPU=1,4,8` y compara cómo escalan las implementaciones.
