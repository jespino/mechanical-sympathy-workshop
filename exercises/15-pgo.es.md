# Optimización Guiada por Perfiles

Hasta ahora hemos usado perfiles para decidir qué código cambiar. Con la **optimización guiada por perfiles (PGO)**, damos un perfil de CPU al compilador para que tome mejores decisiones. En este laboratorio, el código fuente no cambia; solo cambia la compilación.

## Contenido que Quizá Quieras Ver Primero

- [El IR](https://internals-for-interns.com/es/posts/the-go-ir/#devirtualización-profile-guided-pgo) (artículo, Internals for Interns): cómo un perfil permite al compilador devirtualizar llamadas que el análisis estático no puede.

## Objetivos de Aprendizaje

- Recoger un perfil de CPU representativo.
- Compilar y medir con y sin PGO.
- Reconocer cómo más inlining puede eliminar asignaciones.
- Usar `default.pgo` al compilar una aplicación.

## El Concepto: Devolver el Perfil al Compilador

### Deja que las mediciones guíen al compilador

Sin un perfil, el compilador usa reglas generales como el presupuesto de inlining de 80. Un perfil de CPU muestra qué llamadas consumen más tiempo de CPU. PGO permite dedicar más esfuerzo a esas **llamadas frecuentes**, con un presupuesto de inlining de hasta 2000.

![PGO: compila, ejecuta con carga real, captura un perfil de CPU y recompila con él para que el compilador optimice las rutas calientes](images/15-pgo-loop.webp)

### Más que eliminar llamadas

Si una interfaz suele contener un tipo concreto, PGO puede añadir una llamada directa para ese tipo y mantener la llamada a la interfaz para los demás. Esto es **desvirtualización**. La alternativa conserva el comportamiento correcto aunque cambie la carga.

Después, el inlining puede permitir que el escape analysis vea que un objeto solo se necesita localmente. El resultado puede ser menos asignaciones en el heap además de menos llamadas.

### Elegir un perfil

| Flag de compilación | Comportamiento |
|---|---|
| `-pgo=off` | compilar sin PGO |
| `-pgo=cpu.prof` | usar el perfil indicado |
| `-pgo=auto` (predeterminado) | usar `default.pgo` junto al paquete main |

Para este benchmark de una librería, pasa el perfil explícitamente. En una aplicación, pon `default.pgo` junto a `main.go` para usarlo en las compilaciones normales. Recoge perfiles con una carga representativa y renuévalos cuando cambie el uso.

## El Laboratorio

`Aggregator.Ingest` lee logs de peticiones web y pasa cada registro a un `Observer`. Una línea como `17,200,1534` significa que la ruta 17 devolvió HTTP 200 en 1.534 microsegundos. El observer habitual cuenta peticiones por rangos de tiempo de respuesta. El parser inicial es demasiado grande para hacerse inline y reserva un registro por línea. Dejaremos que un perfil guíe al compilador para optimizar esas llamadas.

Este laboratorio no tiene `after.go`. Usa los comandos siguientes en lugar de `make bench LAB=15`. Desde la raíz del repositorio, `make pgo` también ejecuta la comparación principal.

### Paso 1: Mide sin PGO

Ejecuta los comandos restantes desde `labs/`:

```bash
cd labs
go test -pgo=off -run '^$' -bench Ingest -benchmem -count 6 ./15-pgo/ | tee old.txt
```

Fíjate en el tiempo y en `allocs/op`. La versión inicial hace unas 100.000 asignaciones por ejecución.

### Paso 2: Recoge un perfil

```bash
go test -pgo=off -run '^$' -bench Ingest -cpuprofile cpu.prof ./15-pgo/
go tool pprof -top cpu.prof
```

Busca `parseRecord` y trabajo de asignación como `mallocgc`. Este benchmark es nuestra carga de entrenamiento: debe ejecutar el código que queremos mejorar.

### Paso 3: Compila con el perfil

```bash
go build -pgo=cpu.prof -gcflags='labs/15-pgo=-m -d=pgodebug=1' ./15-pgo/ 2>&1 | grep -E 'PGO|inline|Record.*escape'
```

Busca el inlining de `parseRecord` y una llamada directa de PGO a `Histogram.Observe`. En la llamada al parser integrada, `Record` debería dejar de escapar al heap.

### Paso 4: Comprueba y compara

```bash
go test -pgo=cpu.prof ./15-pgo/
go test -pgo=cpu.prof -run '^$' -bench Ingest -benchmem -count 6 ./15-pgo/ | tee new.txt
benchstat old.txt new.txt
```

En mi máquina:

| Medida | Sin PGO | Con PGO |
|---|---|---|
| Tiempo por ejecución | 1,979 ms | 1,004 ms |
| Asignaciones por ejecución | 100.000 | 0 |

## Entendiendo los Resultados

El benchmark es aproximadamente el doble de rápido. La mayor parte de la mejora viene del inlining de `parseRecord`, que permite al escape analysis mantener el registro en el stack. El código fuente no ha cambiado.

Este laboratorio está preparado para que esa interacción se vea con claridad. Otros programas pueden mejorar mucho menos y un perfil de una carga distinta puede no ayudar. Mide tu propia aplicación y comprueba que sus resultados sigan siendo correctos.

## Usando `default.pgo`

Todavía desde `labs/`, prueba la versión de aplicación:

```bash
go run ./15-pgo/cmd/ingest -cpuprofile /tmp/ingest.prof
cp /tmp/ingest.prof 15-pgo/cmd/ingest/default.pgo
go build -o /tmp/ingest-pgo ./15-pgo/cmd/ingest
go version -m /tmp/ingest-pgo | grep pgo
```

Una compilación normal encuentra ahora el perfil junto al paquete main. Usa `-pgo=off` cuando quieras comparar explícitamente sin él.

## Puntos Clave

- PGO usa un perfil de CPU para priorizar las llamadas frecuentes.
- Más inlining también puede eliminar asignaciones en el heap.
- Pasa `-pgo=file` en benchmarks; usa `default.pgo` junto al paquete main de una aplicación.
- Usa perfiles representativos y mide la compilación resultante.

## Para Ir Más Allá (opcional)

1. Devuelve `Record` por valor y mide con `-pgo=off`. ¿Cuánta mejora consigues cambiando el código?
2. Cambia el observer del benchmark a `&Counter{}`, recoge un perfil nuevo e inspecciona las decisiones del compilador.
3. Compara una compilación con el perfil antiguo y otra con el nuevo. ¿Son ambas correctas? ¿Cambian sus tiempos?
