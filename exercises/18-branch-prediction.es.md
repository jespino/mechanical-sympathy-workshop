# Predicción de Saltos

> **¿Quieres saber más?** En Internals for Interns, [De SSA a Código Máquina](https://internals-for-interns.com/es/posts/the-go-machine-code/#generando-control-de-flujo) muestra cómo convierte el compilador las condiciones en saltos, y [SIMD y Go](https://internals-for-interns.com/es/posts/go-simd/#máscaras-y-blending-el-if-de-simd) enseña otra forma de evitar ramas, con máscaras.

La CPU suele adivinar qué camino tomará un `if` para seguir trabajando antes de conocer la condición. Los saltos predecibles son baratos; las predicciones fallidas desperdician trabajo. Compararemos datos ordenados y aleatorios, y probaremos código que no necesite un salto dependiente de los datos.

## Objetivos de Aprendizaje

Al terminar este módulo, podrás:

- Explicar por qué el orden de los datos afecta al código con saltos
- Comparar entradas predecibles e impredecibles
- Comprobar si el compilador genera un salto condicional
- Probar código sin saltos y reconocer sus contrapartidas

## El Concepto: Adivinar el Futuro

### Adivinar el siguiente camino

La CPU trabaja en varias instrucciones a la vez. Ante un salto, predice el siguiente camino y empieza a trabajar en él. Si falla, descarta ese trabajo y empieza por el camino correcto.

![La CPU adivina hacia dónde va un salto y empieza a ejecutar; una predicción fallida tira ese trabajo](images/18-pipeline.webp)

Una condición que casi siempre es verdadera resulta fácil de predecir. Una basada en valores aleatorios es mucho más difícil. Ordenar la entrada puede agrupar todos los resultados falsos y después todos los verdaderos, haciendo más predecible el mismo cálculo.

### Sin saltos no significa gratis

El compilador puede sustituir algunos `if` pequeños por instrucciones que seleccionan un valor sin saltar. En amd64 incluyen los **movimientos condicionales**, que aparecen como `CMOV` en el ensamblador.

No todos los `if` se convierten así. Actualizar varios valores o escribir en memoria puede dejar un salto condicional. Comprueba el código generado en lugar de suponerlo.

El código sin saltos puede hacer trabajo extra por elemento. Puede ayudar con entradas impredecibles y perder frente a un salto fácil de predecir. Importan los datos que uses en el benchmark.

## El Laboratorio

`SumAbove` suma y cuenta precios de productos que alcanzan un precio mínimo. `Filter` crea la lista de precios en ese rango. Usamos unidades monetarias enteras: `[10, 200, 150]` con mínimo 128 suma 350 y conserva `[200, 150]`. Ambas funciones usan el mismo millón de precios, en orden aleatorio y ordenado. Compara los dos órdenes antes de cambiar el código.

Ejecuta los comandos desde la raíz del repositorio.

### Paso 1: Mide la línea base

```bash
make bench LAB=18
```

### Paso 2: Encuentra el coste

```bash
(cd labs && go build -gcflags=-S ./18-branch-prediction/ 2>&1 | grep -A20 'SumAboveBefore STEXT')
```

Busca un salto como `JGT` después de la comparación con el umbral. Ese es el salto dependiente de los datos, distinto del que repite el bucle. Los nombres de las instrucciones dependen de la arquitectura.

Opcional en Linux: usa `perf stat -e branches,branch-misses` con un benchmark compilado para comparar la entrada aleatoria y la ordenada. Si los contadores no están disponibles, sáltalos. Si aún no tienes `perf`, mira la nota de instalación del [módulo 16](16-cpu-caches.md).

### Paso 3: Arréglalo

Edita `labs/18-branch-prediction/after.go`. Mantén `before.go` como referencia.

1. En `SumAboveAfter`, convierte la comparación en un valor 0/1 y actualiza los dos resultados con aritmética usando el ejemplo de abajo. Si `b == 1`, `-int64(b)` tiene todos los bits a uno y conserva el valor; si `b == 0`, la máscara lo convierte en cero.
2. En `FilterAfter`, da longitud suficiente a `out` con `out = out[:len(d)]` (quien llama proporciona la capacidad necesaria). Escribe cada valor en `out[n]` y aumenta `n` en 0 o 1 según la comparación. El siguiente valor sobrescribe uno rechazado. Devuelve `n`, el número de valores conservados.
3. Inspecciona otra vez el ensamblador. Busca que desaparezca el salto dependiente de los datos; el bucle sigue necesitando controlar la repetición.

```go
b := 0
if v >= t {
    b = 1
}
count += b
sum += int64(v) & -int64(b)
```

### Paso 4: Comprueba y compara

```bash
(cd labs && go test ./18-branch-prediction/)
make bench LAB=18
```

Solución de referencia (`make solution LAB=18`), en mi máquina:

| Benchmark | Antes | Después |
|---|---|---|
| `SumAbove`, aleatorio | 3030,9 µs | 481,4 µs |
| `SumAbove`, ordenado | 314,8 µs | 479,5 µs |
| `Filter`, aleatorio | 3199,7 µs | 392,3 µs |
| `Filter`, ordenado | 424,6 µs | 406,4 µs |

## Entendiendo los Resultados

La **entrada aleatoria** mejora mucho porque la CPU ya no toma una decisión difícil de predecir por cada valor. Los tiempos nuevos son parecidos con datos ordenados y aleatorios.

La **entrada ordenada** muestra la contrapartida: `SumAboveAfter` es más lento que antes. El salto original era fácil de predecir y evitaba sumas innecesarias. La versión sin saltos hace esa aritmética en cada iteración.

`Filter` mejora poco con datos ordenados en esta ejecución. Pequeñas diferencias en la distribución del ensamblador afectan a bucles ajustados, así que esa fila no demuestra que quitar saltos siempre gane.

Repetir entradas aleatorias pequeñas puede hacerlas parcialmente predecibles para la CPU. Usa entradas variadas o suficientemente grandes y elige según los datos que procese realmente tu aplicación.

## Puntos Clave

- Los saltos predecibles suelen ser baratos; fallar la predicción desperdicia trabajo.
- El compilador ya elimina algunos saltos simples.
- El código sin saltos puede hacer trabajo extra y ser más lento con datos predecibles.
- Mide con distribuciones realistas y revisa el código generado.

## Para Ir Más Allá (opcional)

1. Haz que el 95% de los valores quede por debajo del umbral. ¿Qué versión es más rápida?
2. Compara `SumAboveSimple` de `before.go` con `SumAboveBefore`. ¿Cuál usa ya selección condicional en tu CPU?
3. Prueba entradas más pequeñas. ¿Afecta al resultado repetir la misma secuencia aleatoria?
