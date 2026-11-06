# Cachés de CPU y Localidad de Datos

Un bucle puede pasar más tiempo esperando a la memoria que haciendo cálculos. Las cachés de CPU mantienen los datos recientes cerca del procesador. En este primer módulo de la Parte IV, cambiaremos cómo se recorren los datos para aprovechar mejor esas cachés.

## Contenido que Quizá Quieras Ver Primero

- [NUMA](https://internals-for-interns.com/es/posts/linux-kernel-numa/) (artículo, Internals for Interns): por qué, en máquinas grandes, parte de la RAM tarda más en alcanzarse que el resto.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Entender los niveles y las líneas de caché
- Reconocer la ventaja de recorrer memoria de forma secuencial
- Comparar slices de valores y de punteros
- Elegir una distribución adecuada para los campos que lee un bucle

## El Concepto: La Jerarquía de Memoria

### Pequeña y rápida, grande y lenta

L1 es una caché pequeña cerca de cada core de CPU. L2 y L3 guardan más datos, pero tardan más en responder. La RAM es todavía mayor y más lenta. Los tamaños y tiempos exactos dependen de tu máquina.

![La jerarquía de memoria: cada nivel es más grande y más lento que el anterior](images/16-hierarchy.webp)

Prueba la demo de latencia. Lee conjuntos de datos cada vez mayores para mostrar cómo sube el tiempo cuando dejan de caber en cada caché:

```bash
(cd labs && go run ./16-cpu-caches/cmd/latency)
```

### Usa datos cercanos

Las cachés traen bloques llamados **líneas de caché**, normalmente de 64 bytes. Leer valores contiguos aprovecha los bytes ya traídos. Reutilizar pronto los mismos datos también ayuda porque pueden seguir en caché.

![Recorrer por filas usa cada byte de cada línea de caché; recorrer por columnas toca una línea nueva en cada acceso](images/16-traversal.webp)

La CPU suele poder traer datos por adelantado si recorres la memoria en un orden predecible. Seguir punteros a objetos dispersos lo dificulta.

### Adapta la distribución al bucle

`[]Account` mantiene juntos los registros; `[]*Account` puede apuntar a registros dispersos por la memoria. Si un bucle solo necesita los saldos, un `[]float64` separado evita leer los demás campos. Mantener un slice por campo se llama **struct de arrays**.

Esta distribución exige más gestión al insertar o actualizar un registro completo. Úsala en bucles importantes medidos que lean pocos campos, no automáticamente para todos los tipos de datos.

## El Laboratorio

Los conjuntos de datos son grandes a propósito. `DailySales` suma las ventas por día en una tabla cuyas filas son tiendas y cuyas columnas son días. La tabla se guarda por filas, pero se lee por columnas. `PtrLedger` sigue punteros a cuentas en orden mezclado. `Ledger` lee saldos de structs completos de cuentas. Mejoraremos cada patrón de acceso.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=16
```

### Paso 2: Encuentra el coste

Lee los tres bucles de `before.go`. Pregúntate: ¿el siguiente valor está al lado del actual o en otro lugar de la memoria? ¿Usa el bucle todos los campos que carga?

Opcional en Linux: después del cambio, compara los fallos de caché de las dos versiones.

> **¿Necesitas `perf`?** Solo existe en Linux. Instálalo con `sudo apt install linux-tools-common linux-tools-$(uname -r)` (Debian/Ubuntu) o `sudo dnf install perf` (Fedora). Si `perf stat true` falla con un error de permisos, ejecuta `sudo sysctl kernel.perf_event_paranoid=1`. En macOS, Windows, o una VM o contenedor sin contadores hardware, sáltate este paso.

```bash
(cd labs && go test -c -o caches.test ./16-cpu-caches/)
(cd labs && perf stat -e cache-misses ./caches.test -test.run '^$' -test.bench 'DailySales/impl=before' -test.benchtime 20x)
(cd labs && perf stat -e cache-misses ./caches.test -test.run '^$' -test.bench 'DailySales/impl=after' -test.benchtime 20x)
```

Si los contadores no están disponibles, usa los tiempos del benchmark. Los contadores incluyen la preparación de datos, así que son evidencia de apoyo y no una cuenta exacta de los fallos del bucle.

### Paso 3: Arréglalo

Edita `labs/16-cpu-caches/after.go`. Mantén `before.go` como referencia.

1. En `DailySalesAfter`, recorre tiendas en el bucle exterior y días en el interior, sumando cada valor a `sums[j]`. Mantén el orden original de las sumas de cada columna.
2. En `PtrLedgerAfter`, guarda `[]Account` en lugar de `[]*Account` y rellénalo directamente en `NewPtrLedgerAfter`.
3. En `LedgerAfter`, usa un slice por campo. Adapta la construcción y haz que `Total` recorra solo el slice de saldos.

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./16-cpu-caches/)
make bench LAB=16
```

Solución de referencia (`make solution LAB=16`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| `DailySales` | 77,889 ms | 8,286 ms |
| `PtrLedger` | 3,614 ms | 1,386 ms |
| `Ledger` | 1392 µs | 289 µs |

## Entendiendo los Resultados

**DailySales** es unas 9 veces más rápido porque lee la tabla de ventas en el orden en que está guardada. Cada línea de caché aporta varios valores útiles en lugar de uno.

**PtrLedger** es unas 2,6 veces más rápido porque las cuentas quedan juntas en vez de acceder a ellas mediante punteros dispersos. **Ledger** es unas 4,8 veces más rápido porque solo lee saldos y no registros completos.

Estos recorridos medidos no asignan memoria. Las mejoras vienen del acceso a los datos. Repetir el benchmark también puede calentar la caché, así que prueba tamaños y patrones parecidos al uso real.

## Puntos Clave

- Recorre los datos en el orden en que están guardados.
- Prefiere valores contiguos para recorridos grandes cuando el diseño lo permita.
- Separa campos leídos con frecuencia si reduce mucho los datos que hay que cargar.
- Mide tamaños realistas, no solo una caché caliente.

## Para Ir Más Allá (opcional)

1. Pon `matrixN` a `256`. ¿Cómo cambia la ventaja del orden por filas con una tabla de ventas pequeña?
2. En una copia de prueba, quita la mezcla de punteros de la versión inicial. ¿Acelera `PtrLedger`?
3. Compara la demo de latencia con los tiempos de los recorridos. ¿Por qué afecta menos la latencia individual a los recorridos secuenciales?
