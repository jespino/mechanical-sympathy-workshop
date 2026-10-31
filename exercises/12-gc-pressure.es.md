# Ayudando al Garbage Collector

Reducir asignaciones ayuda al recolector de basura, pero también importan los datos que mantenemos vivos. Una colección grande de objetos unidos por punteros cuesta trabajo de recorrer en cada ciclo de GC. Cambiaremos la distribución de un almacén para reducir ese trabajo repetido.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Entender por qué los objetos vivos con punteros añaden trabajo al GC
- Usar valores e índices para reducir el almacenamiento con muchos punteros
- Leer la información principal de una traza de GC
- Explorar el equilibrio entre memoria y CPU con `GOGC` y `GOMEMLIMIT`

## El Concepto: Lo Que Hace Realmente el GC

### Qué se recorre

El GC empieza por los valores a los que el programa todavía puede acceder, como variables locales y globales. Sigue punteros para encontrar objetos vivos y recupera los que ya no son accesibles. Gran parte ocurre mientras tu programa sigue ejecutándose: pausas cortas no significan poco consumo de CPU.

![El GC sigue todos los punteros del heap; la memoria sin punteros nunca se escanea](images/12-mark.webp)

Un `[]*Record` contiene punteros a muchos registros separados. Los campos string también apuntan a sus bytes. El GC debe revisar esas referencias en cada ciclo.

La memoria sin punteros, como el array de un `[]byte` o `[]float64`, no necesita que se recorra su contenido. Se llama memoria **noscan**. El objeto sigue teniendo que mantenerse vivo, pero el GC no busca referencias dentro de él.

### Menos punteros, menos trabajo repetido

Prefiere `[]Record` a `[]*Record` si no necesitas que cada objeto tenga identidad independiente. Un índice entero puede sustituir un puntero a ese slice. El texto se puede guardar en un solo slice de bytes con posiciones y longitudes, en lugar de muchos strings individuales.

Estos cambios complican el código. Son útiles para datos grandes y de larga duración que los perfiles señalen como caros de recorrer, no para todos los structs.

### Ajustes de memoria y CPU

![El objetivo del heap es el heap vivo más un GOGC por ciento; un ciclo de GC empieza cuando el heap se acerca al objetivo](images/12-pacer.webp)

`GOGC` controla cuánto puede crecer el heap respecto a los datos vivos. Un valor mayor suele usar más memoria y ejecutar menos ciclos de GC; uno menor hace lo contrario. El valor predeterminado es 100.

`GOMEMLIMIT` es un **límite flexible** de la memoria gestionada por el runtime de Go, no un tope estricto de memoria del proceso. Si queda demasiado cerca del tamaño de los datos vivos, el GC puede gastar mucha CPU intentando hacer sitio.

Las versiones modernas de Go también mejoran el recorrido con el recolector Green Tea. La distribución de datos sigue importando porque evita trabajo en lugar de solo acelerarlo.

## El Laboratorio

`StoreBefore` conserva dos millones de usuarios usando `[]*Record` y `map[int64]*Record`. `GC` mide una recolección con el almacén vivo, `Churn` añade asignaciones temporales a su lado y `AverageScore` recorre los datos. Construir el almacén hace que este laboratorio tarde más que los anteriores.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=12
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go test -c -o gc.test ./12-gc-pressure/ && GODEBUG=gctrace=1 ./gc.test -test.run '^$' -test.bench 'GC/impl=before' -test.benchtime 3x)
```

Cada línea `gc` es una recolección. En `286->286->286 MB`, el último número es el heap vivo. Los tres números antes de `ms clock` indican la pausa inicial, el tiempo de marcado concurrente y la pausa final. Los valores `ms cpu` incluyen trabajo de varios cores y pueden ser mucho mayores que el tiempo transcurrido.

### Paso 3: Arréglalo

Edita `labs/12-gc-pressure/after.go`. Mantén `before.go` como referencia.

1. Guarda nombres y emails en un único `text []byte`. Conserva sus posiciones y longitudes en `recordAfter`; reconstruye el nombre en `Lookup` con `string(s.text[off:off+uint32(length)])`.
2. Guarda los registros por valor en `[]recordAfter`, en lugar de un slice de punteros.
3. Cambia `byID` a `map[int64]int32`, asociando cada ID al índice de su registro. Adapta la construcción, `Lookup` y `AverageScore`. Mide con `BENCH=GC` después de cada cambio.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./12-gc-pressure/)
make bench LAB=12
```

Solución de referencia (`make solution LAB=12`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| `GC` | 22,77 ms | 0,387 ms |
| `Churn` | 1367,5 ns | 819,4 ns |
| `AverageScore` | 7,627 ms | 1,409 ms |

## Entendiendo los Resultados

El GC completo es unas 59 veces más rápido aunque el almacén siga usando mucha memoria. El cambio principal es cuántos punteros debe seguir el GC, no solo el tamaño del heap.

`Churn` también mejora porque sus asignaciones temporales ya no provocan recorridos caros del almacén antiguo. `AverageScore` acelera porque los registros quedan juntos en memoria.

La contrapartida es tener que gestionar más detalles. Modificar o borrar texto es más difícil y `Lookup` ahora reserva el string que devuelve. Usa esta distribución donde los recorridos repetidos del GC sean un problema medido.

## Prueba los ajustes del GC

La demo muestra ciclos de GC, CPU del GC, tiempo transcurrido y memoria máxima:

```bash
(cd labs && GOGC=50 go run ./12-gc-pressure/cmd/gctune)
(cd labs && GOGC=200 go run ./12-gc-pressure/cmd/gctune)
(cd labs && GOGC=off GOMEMLIMIT=400MiB go run ./12-gc-pressure/cmd/gctune)
```

Más memoria puede reducir la frecuencia del GC. Reducir punteros hace más barato cada ciclo; es una mejora distinta.

## Puntos Clave

- Los punteros vivos añaden trabajo de recorrido en cada ciclo de GC.
- Los valores, índices y el texto compartido pueden reducir ese trabajo.
- `GOGC` cambia memoria por frecuencia de recolección; `GOMEMLIMIT` es un límite flexible.
- Mide la CPU del GC y el trabajo normal del programa, no solo las pausas.

## Para Ir Más Allá (opcional)

1. Ejecuta la demo con `GOGC=50`, `100` y `200`. Compara el uso de memoria y la CPU del GC.
2. Prueba un `GOMEMLIMIT` menor. ¿Necesita más CPU el GC para mantenerse cerca de él?
3. Añade un campo con puntero a `recordAfter`. ¿Cómo cambia el benchmark `GC`?
