# sync.Pool

Un buffer temporal puede ser barato de usar, pero caro de crear en cada petición. `sync.Pool` permite que las goroutines reutilicen objetos temporales entre llamadas. Usaremos un pool para un buffer de logs y mediremos las asignaciones y el trabajo del GC que evita.

## Contenido que Quizá Quieras Ver Primero

- [sync.Pool in Go Explained & Avoid The Heap Allocation Mistake](https://www.youtube.com/watch?v=fwHok9ZhQaY) (vídeo de Phuong Le, en inglés): cuándo usar un pool, cuándo se vacían sus objetos y qué trampas de asignación evitar.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Usar `Get`, `Put` y `New` para reutilizar objetos temporales
- Entender que los objetos del pool pueden desaparecer en cualquier momento
- Reiniciar objetos y evitar retener buffers demasiado grandes
- Medir el pool en benchmarks secuenciales y paralelos

## El Concepto: Cómo Funciona `sync.Pool`

### Obtener, usar y devolver

`Get` toma un objeto disponible o llama a `New` para crearlo. Después de usarlo, `Put` lo deja disponible para otra llamada. El pool se puede compartir entre goroutines, pero cada objeto prestado debe tener un único dueño cada vez.

El runtime mantiene almacenamiento local para sus recursos de ejecución de Go, llamados **P**. Así la mayoría de las operaciones del pool evitan competir por una sola cola compartida.

![sync.Pool mantiene un slot privado y una cola compartida por cada P, y otros P pueden robar de las colas compartidas](images/11-pool.webp)

### Los objetos no se conservan para siempre

El recolector de basura puede quitar objetos del pool. La implementación actual conserva una ronda anterior de objetos en una **caché de víctimas**, pero es un detalle de implementación, no una promesa de que un objeto sobreviva.

![Un objeto en el pool sobrevive a un GC en la victim cache y se descarta en el segundo](images/11-victim.webp)

Puedes observar el comportamiento actual con:

```bash
(cd labs && go run ./11-sync-pool/cmd/victim)
```

`New` debe poder recrear siempre un objeto. Usa los pools para buffers temporales, no para recursos como archivos abiertos o conexiones.

### Reglas para reutilizar con seguridad

- Guarda punteros, como `*bytes.Buffer`. Pasar un slice por valor mediante `any` puede reservar memoria.
- Reinicia el objeto antes de reutilizarlo para que los datos anteriores no entren en el siguiente resultado.
- Devuelve solo buffers de un tamaño razonable; una petición grande no debería dejar un buffer enorme retenido.
- Después de `Put`, deja de usar el objeto y cualquier slice que apunte a su memoria.

## El Laboratorio

`WriteEventBefore` construye una línea de log JSON en un `bytes.Buffer` nuevo en cada llamada. `WriteEvent` mide una goroutine y `WriteEventParallel` mide llamadas concurrentes. La columna extra `GCs/Mop` indica ciclos de GC por millón de operaciones.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=11
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go test -run '^$' -bench 'WriteEventParallel/impl=before' -memprofile mem.prof ./11-sync-pool/ && go tool pprof -sample_index=alloc_space -top mem.prof)
```

Busca `bytes.growSlice` y el crecimiento del buffer. La versión inicial hace siete asignaciones por llamada, sobre todo para construir la línea temporal.

### Paso 3: Arréglalo

Edita `labs/11-sync-pool/after.go`. Mantén `before.go` como referencia.

1. Importa `sync` y declara un `sync.Pool` a nivel de paquete cuyo `New` devuelva `new(bytes.Buffer)`.
2. En `WriteEventAfter`, obtén el buffer con `pool.Get().(*bytes.Buffer)`, reinícialo, codifica el evento y escríbelo.
3. Devuélvelo al pool después de escribir, también en las rutas de error, salvo que `buf.Cap()` supere `64*1024`. No uses sus bytes después de devolverlo.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./11-sync-pool/)
make bench LAB=11
```

Solución de referencia (`make solution LAB=11`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| `WriteEvent` | 879,1 ns | 563,4 ns |
| `WriteEventParallel` | 274,60 ns | 48,19 ns |
| Asignaciones por llamada | 7 | 0 |

## Entendiendo los Resultados

El pool evita crear y ampliar buffers una y otra vez. Tras el calentamiento, el benchmark llega a cero asignaciones por llamada y reduce el trabajo del GC. No garantiza cero asignaciones para siempre: el pool puede perder objetos y crear otros.

El benchmark paralelo mejora más porque muchas goroutines dejan de asignar memoria a la vez. El trabajo restante consiste sobre todo en formatear y escribir el evento.

Se puede devolver el buffer cuando termina `w.Write` porque un `io.Writer` no debe conservar los bytes recibidos. Si un objeto ya se queda en el stack, normalmente no necesita un pool.

## Puntos Clave

- Usa pools para objetos temporales cuya creación repetida sea cara según los perfiles.
- Guarda punteros y reinicia los objetos antes de reutilizarlos.
- Los objetos pueden desaparecer del pool en cualquier momento.
- Limita el tamaño retenido y deja de usar los objetos después de `Put`.

## Para Ir Más Allá (opcional)

1. Ejecuta `make bench LAB=11 CPU=1,2,4,8`. ¿Cómo cambia el rendimiento paralelo con el pool?
2. Añade un evento grande de vez en cuando. Compara la memoria con y sin el límite de tamaño.
3. Ejecuta `cmd/victim` y observa qué pasa después de forzar ciclos de GC.
