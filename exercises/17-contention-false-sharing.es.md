# Contención y False Sharing

Más workers no siempre significan más progreso. Si todas las goroutines actualizan el mismo contador, compiten por la misma memoria. Compararemos un mutex, un contador atómico y contadores separados por worker, y después arreglaremos la compartición oculta entre contadores cercanos.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Reconocer la contención en un contador compartido
- Entender por qué una variable atómica puede seguir limitando el escalado
- Explicar el false sharing entre variables cercanas
- Dividir un contador en partes con relleno y medir con distintos números de CPU

## El Concepto: Cores Esperándose Unos a Otros

### Esperar por un mutex

Un mutex permite que solo una goroutine actualice el contador cada vez. Si muchas lo quieren, pasan tiempo esperando y despertándose. El perfil de mutex muestra esas esperas.

### Por qué no basta con un atómico

Un incremento atómico evita un mutex explícito, pero las actualizaciones de una variable siguen necesitando un orden coordinado. Cada core tiene su propia caché y la línea que contiene el contador debe pasar entre los cores que escriben en ella. Ese tráfico puede costar más que el incremento diminuto.

### Variables distintas pueden compartir memoria

Dar a cada worker su contador ayuda, pero los contadores contiguos pueden ocupar la misma **línea de caché**. En la máquina de este laboratorio, una línea tiene 64 bytes y caben ocho contadores `int64`. Cores distintos pueden interferir aunque escriban variables diferentes. Esto es **false sharing**.

![Dos núcleos que escriben contadores distintos en la misma línea de caché se la pasan de uno a otro; el padding da a cada contador su propia línea](images/17-false-sharing.webp)

Separa los contadores con relleno para que ocupen líneas distintas:

```go
type paddedShard struct {
    n atomic.Int64
    _ [56]byte
}
```

Este ejemplo supone líneas de 64 bytes. Otro hardware puede necesitar distinta separación. El relleno usa más memoria: aplícalo a escrituras compartidas que las mediciones señalen como caras.

## El Laboratorio

`Inc(worker)` incrementa un contador y `Load` lee el total. El benchmark incluye `before` (mutex), `atomic` (un atómico compartido), `sharded` (contadores contiguos por worker) y `after` (tu versión). `steps.go` contiene las implementaciones intermedias.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=17 CPU=1,2,4,8
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go test -run '^$' -bench 'Inc/impl=before' -mutexprofile mutex.out ./17-contention-false-sharing/ && go tool pprof -top -cum mutex.out)
```

Busca tiempo atribuido al lock del contador. Las versiones atómicas pueden no mostrar esperas de mutex y aun así escalar mal: el tráfico de líneas de caché es otro coste.

### Paso 3: Arréglalo

Edita `labs/17-contention-false-sharing/after.go`. Mantén `before.go` como referencia.

1. Importa `sync/atomic` y parte del diseño de `ShardedCounter` en `steps.go`. Reserva `numShards()` contadores y pon `mask` al número de contadores menos uno.
2. Usa `paddedShard` en lugar de `atomic.Int64` como elemento del slice. Incrementa `c.shards[worker&c.mask].n` con `Add(1)`.
3. Implementa `Load` sumando las partes con lecturas atómicas. Mantén los atómicos porque puede haber lecturas mientras los workers escriben.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./17-contention-false-sharing/)
make bench LAB=17 CPU=1,2,4,8
```

Solución de referencia (`make solution LAB=17 CPU=1,2,4,8`), en mi máquina:

| CPU | Mutex | Atómico compartido | Partes contiguas | Partes con relleno |
|---|---|---|---|---|
| 1 | 8,551 ns | 4,228 ns | 4,385 ns | 4,445 ns |
| 4 | 26,880 ns | 7,476 ns | 7,118 ns | 1,126 ns |
| 8 | 47,025 ns | 9,284 ns | 10,630 ns | 0,5766 ns |

## Entendiendo los Resultados

Lee cada columna hacia abajo para ver cómo escala. El mutex y el atómico compartido empeoran con más CPU. Las partes contiguas todavía interfieren porque comparten líneas de caché. El relleno permite actualizar memoria separada con mucha menos interferencia.

Son tiempos de rendimiento total: tiempo transcurrido dividido entre todos los incrementos terminados. Un valor menor de un nanosegundo no significa que cada incremento individual tarde eso.

La contrapartida es un contador mayor y un `Load` más lento, que suma todas las partes. Un `Load` concurrente no es una foto de todas ellas en el mismo instante. Es útil para métricas, pero no para decidir si se puede reservar la última entrada disponible.

## Puntos Clave

- Un atómico compartido puede seguir siendo un cuello de botella.
- El false sharing aparece cuando escrituras independientes comparten una línea de caché.
- Separa las escrituras frecuentes y combina los resultados menos a menudo.
- Mide con varios números de CPU, no solo con uno.

## Para Ir Más Allá (opcional)

1. Prueba partes de 128 bytes. ¿Aprovecha tu máquina ese espacio extra?
2. Compara `CPU=1,2,4,8,16` cuando estén disponibles. ¿Cuándo deja de escalar bien?
3. En una copia de prueba, sustituye el mutex por `sync.RWMutex`. ¿Mejora una carga dominada por incrementos?
