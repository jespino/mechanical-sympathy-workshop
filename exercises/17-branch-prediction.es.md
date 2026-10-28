# Predicción de Saltos

> **¿Quieres saber más?** El pase del compilador de Go que convierte saltos en movimientos condicionales es [`cmd/compile/internal/ssa/branchelim.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/ssa/branchelim.go): es corto y muy fácil de leer. La famosa pregunta de Stack Overflow [Why is processing a sorted array faster than processing an unsorted array?](https://stackoverflow.com/questions/11227809) es la explicación clásica. La [guía de microarquitectura](https://www.agner.org/optimize/microarchitecture.pdf) de Agner Fog documenta los branch predictors y las penalizaciones por fallo de predicción de cada familia de CPU. [Branch prediction](https://danluu.com/branch-prediction/), de Dan Luu, es una introducción amable a cómo funcionan los predictores.

Una CPU no espera a saber hacia dónde va un `if`. Adivina, y sigue ejecutando por el camino que ha adivinado. Cuando acierta, el salto sale prácticamente gratis. Cuando falla, todo el trabajo hecho por el camino equivocado se tira a la basura. En este módulo vemos cuánto cuesta eso, cuándo el compilador de Go elimina saltos por ti, y cómo eliminar los que no elimina.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Entenderás el pipeline, la ejecución especulativa y el coste de un fallo de predicción
- Reproducirás en Go el efecto clásico de "los datos ordenados van más rápido"
- Sabrás cuándo el compilador de Go convierte un `if` en un movimiento condicional (`CMOV`), y cuándo no puede
- Leerás el ensamblador de Go con `go build -gcflags=-S` para buscar saltos
- Reescribirás saltos que dependen de los datos sin saltos, y sabrás cuándo eso es más lento
- Contarás fallos de predicción con `perf stat` (Linux)

## El Concepto: Adivinar el Futuro

### Pipelines y especulación

Un core moderno trabaja en decenas de instrucciones a la vez: las trae, decodifica, ejecuta y retira en un pipeline profundo, con una ventana de ejecución fuera de orden de cientos de instrucciones. Para mantener llena esa maquinaria, el front end necesita saber qué instrucción viene después de un salto condicional **mucho antes de que se calcule la condición**. Así que se lo pregunta al **branch predictor**, un componente hardware que recuerda el historial de cada salto y de los saltos que llevaron hasta él, y empieza a traer instrucciones del destino predicho.

![La CPU adivina hacia dónde va un salto y empieza a ejecutar; una predicción fallida tira ese trabajo](images/17-pipeline.webp)

Si la predicción era incorrecta, el core lo descubre cuando por fin se ejecuta la comparación, descarta todo lo traído después del salto y vuelve a empezar desde el destino correcto. En los cores x86 actuales eso cuesta del orden de **15-20 ciclos** de trabajo perdido, más el trabajo útil que el motor fuera de orden podría haber estado haciendo mientras tanto.

Los predictores son muy buenos con los patrones: saltos de bucles, saltos que casi siempre van hacia el mismo lado, e incluso secuencias que se repiten. Lo que no pueden predecir son **datos aleatorios**. Un salto que depende de lanzar una moneda falla la mitad de las veces, por muy listo que sea el predictor.

### Código sin saltos: movimientos condicionales

La alternativa es calcular ambos resultados y *seleccionar* uno, sin salto. En amd64 eso es `CMOVcc` (movimiento condicional) y `SETcc` (poner un byte a 0 o 1 según un flag). No hay que adivinar, así que no hay nada que fallar, pero la selección tiene que esperar a la comparación, y siempre se calculan los dos lados. Un salto que podrías predecir perfectamente es más barato que un movimiento condicional. Un salto que no puedes predecir es mucho más caro.

### Qué hace el compilador de Go

El pase SSA `branchelim` convierte los `if` sencillos en valores `CondSelect`, que se convierten en `CMOV` en amd64. El comentario al principio de `branchelim.go` describe la forma que busca:

```go
// branchelim tries to eliminate branches by
// generating CondSelect instructions.
//
// Search for basic blocks that look like
//
//	bb0            bb0
//	 | \          /   \
//	 | bb1  or  bb1   bb2    <- trivial if/else blocks
//	 | /          \   /
//	bb2            bb3
//
// where the intermediate blocks are mostly empty (with no side-effects);
// rewrite Phis in the postdominator as CondSelects.
```

Leyendo el resto del fichero, las condiciones son estrictas:

- **La rama tiene que ser diminuta**: para un `if` de una sola rama, `const maxfuseinsts = 2` valores SSA como mucho.
- **Sin efectos secundarios**: `canSpeculativelyExecute` rechaza operaciones de memoria (así que **nada de escrituras** en la rama), divisiones y cualquier cosa que pueda hacer panic.
- **Solo enteros y punteros**: `canCondSelect` devuelve false para floats y para valores más grandes que un registro.
- **En amd64, el if/else tiene un coste**: `shouldElimIfElse` permite como mucho un `CMOV` por rombo cuando otros valores dependen de las ramas (`const maxcost = 2`).
- **No para direcciones de carga**: si el valor seleccionado se usa para calcular la dirección de una carga, el salto se queda (issue 26306), porque un salto predicho permite que la carga empiece antes.

Así que esta función del laboratorio ya no tiene saltos:

```go
func SumAboveSimple(d []int32, t int32) int64 {
    var sum int64
    for _, v := range d {
        if v >= t {
            sum += int64(v)
        }
    }
    return sum
}
```

```bash
cd labs
go build -gcflags=-S ./17-branch-prediction/ 2>&1 | grep -A20 'SumAboveSimple STEXT'
```

```
MOVL    (AX)(DX*4), SI      // v := d[i]
MOVLQSX SI, R8
ADDQ    CX, R8              // R8 = sum + v (se calcula siempre)
CMPL    DI, SI              // t frente a v
CMOVQLE R8, CX              // sum = R8 si t <= v
INCQ    DX
CMPQ    BX, DX
JGT     loop
```

El truco clásico de "ordena los datos primero" no hace nada por este bucle en Go. Pero cambia un poco el cuerpo, para que actualice **dos** variables o **escriba** en memoria, y el salto vuelve. De eso va el laboratorio.

> `go tool objdump -s 'SumAbove' ./x.test` muestra el mismo código a partir de un binario compilado, con direcciones. Usa `-gcflags=-S` mientras editas, y `objdump` para comprobar lo que realmente se ha generado.

## El Laboratorio

```
labs/17-branch-prediction/
  before.go      bucles con saltos (línea base, no editar)
  after.go       tu copia: elimina los saltos que dependen de los datos
  solution.go    solución de referencia (se compila con -tags solution)
  lab_test.go    test de corrección + benchmarks
```

| Función | Qué hace | Por qué sobrevive el salto |
|---|---|---|
| `SumAbove(d, t)` | suma **y cuenta** los valores `>= t` | cambian dos variables dentro de la rama |
| `Filter(d, out, t)` | copia los valores `>= t` a `out` | una escritura dentro de la rama |

Cada benchmark se ejecuta sobre los **mismos** 1M de valores (uniformes en `[0, 256)`, umbral 128) en dos órdenes: `data=random` y `data=sorted`.

### Paso 1: Mide la línea base

```bash
make bench LAB=17
```

Antes de cambiar nada, mira la columna `before`: `data=sorted` debería ser muchas veces más rápido que `data=random`. Mismos valores, mismo trabajo, mismo resultado. La única diferencia es si el branch predictor puede acertar.

### Paso 2: Encuentra el salto (y cuenta los fallos)

Mira el ensamblador de `SumAboveBefore`:

```bash
cd labs
go build -gcflags=-S ./17-branch-prediction/ 2>&1 | grep -A20 'SumAboveBefore STEXT'
```

```
MOVL    (AX)(DX*4), R8      // v := d[i]
CMPL    DI, R8
JGT     next                // si t > v, saltar: el salto que depende de los datos
MOVLQSX R8, R8
ADDQ    R8, CX              // sum += v
INCQ    SI                  // count++
JMP     next
```

Después, si quieres, cuenta los fallos de predicción.

> **perf solo existe en Linux.** En macOS y Windows, sáltate esta parte: los tiempos de random frente a sorted ya lo demuestran. En macOS, la plantilla *CPU Counters* de Instruments puede muestrear fallos de predicción si quieres ir más allá.
>
> Si `perf stat` dice *"No supported events found"* o *"Access to performance monitoring … is limited"*, tu distribución lo restringe (Ubuntu y Debian usan `kernel.perf_event_paranoid=4` por defecto). Bájalo hasta el próximo reinicio con `sudo sysctl kernel.perf_event_paranoid=1`.

```bash
go test -c -o bp.test ./17-branch-prediction/
perf stat -e branches,branch-misses \
    ./bp.test -test.run '^$' -test.bench 'SumAbove/data=random/impl=before' -test.benchtime 200x
perf stat -e branches,branch-misses \
    ./bp.test -test.run '^$' -test.bench 'SumAbove/data=sorted/impl=before' -test.benchtime 200x
```

*(Ejecuta ambos y compara. Apunta aquí tus números.)*

Qué buscar: `branches` debería ser parecido en las dos ejecuciones, mientras que `branch-misses` debería ser muchísimo mayor con `data=random`. Con 200 ejecuciones sobre 1M de lanzamientos de moneda, espera del orden de 100M de fallos con datos aleatorios, frente a una cantidad minúscula con datos ordenados. Cuando hayas hecho el Paso 3, ejecuta `impl=after` sobre datos aleatorios: los fallos deberían desplomarse, aunque los datos sigan siendo aleatorios.

### Paso 3: Arréglalo

Edita `labs/17-branch-prediction/after.go`:

1. **`SumAboveAfter`**: saca un valor 0/1 de la comparación con un `if` que el compilador *sí* puede convertir (una variable, valores constantes):

   ```go
   b := 0
   if v >= t {
       b = 1
   }
   ```

   y después actualiza los dos acumuladores con aritmética: `count += b`, y `sum += int64(v) & -int64(b)` (`-1` son todo unos, así que la máscara conserva `v`; `-0` es cero).
2. **`FilterAfter`**: escribe **cada** valor en `out[n]`, y avanza `n` solo si ha pasado el filtro. Un valor rechazado simplemente se sobrescribe con el siguiente. (Por eso `out` tiene que tener sitio para `len(d)` valores: haz primero `out = out[:len(d)]`, que además elimina un bounds check, ver [Eliminación de Bounds Checks](09-bounds-check-elimination.md).)

Vuelve a mirar el ensamblador. Deberías ver `SETLE` (o `SETGE`) y ningún salto condicional en el cuerpo del bucle, aparte de la condición del bucle y el bounds check.

### Paso 4: Compara

```bash
make bench LAB=17
```

Solución de referencia (`make solution LAB=17`) en un AMD Ryzen AI MAX+ 395:

```
                        │    before    │               after                │
                        │    sec/op    │   sec/op     vs base               │
SumAbove/data=random-32   3030.9µ ± 1%   481.4µ ± 2%  -84.12% (p=0.002 n=6)
SumAbove/data=sorted-32    314.8µ ± 3%   479.5µ ± 2%  +52.32% (p=0.002 n=6)
Filter/data=random-32     3199.7µ ± 1%   392.3µ ± 1%  -87.74% (p=0.002 n=6)
Filter/data=sorted-32      424.6µ ± 3%   406.4µ ± 3%   -4.29% (p=0.002 n=6)
```

## Entendiendo los Resultados

**Datos aleatorios, before**: 3,0 ms para 1M de elementos, ~2,9 ns por elemento. **Datos ordenados, before**: 0,31 ms. Son **~10 veces** para las mismas instrucciones. Con los datos ordenados, el salto va hacia un lado durante la primera mitad y hacia el otro durante la segunda: dos fallos de predicción en total. Con datos aleatorios son unos 500.000. (3,0 ms - 0,31 ms) / 500.000 ≈ **5,4 ns perdidos por cada fallo de predicción** en esta máquina: el vaciado del pipeline, más el trabajo fuera de orden que no se pudo solapar.

**After, datos aleatorios**: 6,3 veces más rápido en `SumAbove`, 8,2 veces en `Filter`. Y fíjate en que los tiempos de `after` son **iguales con datos aleatorios y ordenados**. Al código sin saltos le dan igual los datos: esa es precisamente la idea.

**After, datos ordenados**: `SumAbove` es **más lento**, un 52% (y `Filter` también, en cuanto quitas el artefacto de alineamiento que se explica más abajo). Cuando el salto es perfectamente predecible, el bucle con saltos se salta el trabajo de la mitad de los elementos sin coste, mientras que el bucle sin saltos siempre hace el trabajo de todos los elementos, y tiene una cadena de dependencias más larga a través de `SETcc`, `NEG`, `AND`, `ADD`. No es un fallo del laboratorio, es la lección: **el código sin saltos cambia el mejor caso por un peor caso predecible**. Úsalo cuando la condición sea realmente impredecible (datos aleatorios, hashes o controlados por el usuario), y mide con datos realistas.

> **Una trampa de benchmarking que encontramos al construir este laboratorio.** Con 32K elementos en lugar de 1M, el `SumAbove` sobre datos en orden aleatorio era solo ~3,6 veces más lento que sobre datos ordenados, en lugar de ~10 veces. El benchmark ejecuta la misma función sobre los mismos datos miles de veces, y los predictores modernos tienen historial suficiente para **memorizar parcialmente** una secuencia de 32K resultados "aleatorios". Los datos de producción no se repiten así. Si haces benchmarks de código con saltos, usa entradas lo bastante grandes (o variadas) para que el predictor no pueda aprenderlas.

> **Una segunda trampa, encontrada al pasar el taller a Go 1.27.** `FilterBefore` se compila exactamente a las mismas instrucciones que con Go 1.26, y aun así `Filter/data=sorted/impl=before` pasó de ~250 µs a ~425 µs, lo que hace que la versión sin saltos parezca tan rápida como el salto predecible. La causa es el **alineamiento del código**: el linker alinea las funciones a 32 bytes en amd64 (`funcAlign` en `cmd/link/internal/amd64/l.go`), y en el binario de test de Go 1.27 `FilterBefore` empieza justo 32 bytes después de un límite de 64 bytes, así que su bucle caliente de 32 bytes queda a caballo entre dos bloques de código de 64 bytes, que el front end lee y cachea peor. Nada de tu código ha cambiado: el binario creció en otra parte y la función se movió. Recompila con alineamiento de 64 bytes y vuelve la imagen de antes: `go test -tags solution -ldflags=-funcalign=64 -run '^$' -bench Filter ./17-branch-prediction/` da 250,8 µs before y 405,9 µs after con datos ordenados (+62%). Cuando un bucle pequeño se vuelve 1,7 veces más lento y el ensamblador no ha cambiado, sospecha del alineamiento antes que del compilador.

## Puntos Clave

- La CPU predice cada salto y ejecuta de forma especulativa: los aciertos son gratis, los fallos cuestan ~15-20+ ciclos
- Los saltos que dependen de datos aleatorios fallan ~50% de las veces; los datos ordenados o sesgados se predicen bien
- El pase `branchelim` del compilador de Go emite `CMOV` para `if` diminutos, sin efectos secundarios y con enteros: compruébalo con `go build -gcflags=-S`
- Las escrituras, varias variables actualizadas, los floats y las direcciones de carga mantienen el salto
- Reescribe con un valor 0/1 de una comparación más aritmética (máscaras, `+= b`), o con escrituras incondicionales, y con las funciones built-in `min`/`max`
- El código sin saltos es más lento cuando el salto era predecible: decide según tus datos reales
- `perf stat -e branches,branch-misses` muestra directamente el número de fallos de predicción (Linux)

## Para Ir Más Allá (opcional)

1. Sustituye los datos uniformes por una distribución sesgada (por ejemplo, el 95% de los valores por debajo del umbral). ¿Dónde está el punto de equilibrio entre `before` y `after`?
2. Escribe un bucle de clamp `out[i] = min(max(d[i], lo), hi)` y mira el ensamblador. ¿Tiene saltos?
3. Cambia `SumAboveSimple` para que acumule en un `float64`. ¿Sobrevive el `CMOVQLE`? Mira la función `canCondSelect` en `branchelim.go` para ver por qué.
4. Cambia `size` en `lab_test.go` a `1 << 12`, `1 << 15` y `1 << 18`. ¿A partir de qué tamaño deja el predictor de memorizar los datos aleatorios en tu máquina?
5. Go 1.27 añade un paquete experimental y portable [`simd`](https://pkg.go.dev/simd) (compila con `GOEXPERIMENT=simd`). El código sin saltos está a un paso del código paralelo de datos: escribe un `SumAbove` que cargue vectores `simd.Int32s` con `simd.LoadInt32s`, los compare con `GreaterEqual` y acumule con `Masked` y `Add`, de modo que se comparan muchos elementos por instrucción y no hay nada que predecir. ¿Cómo se compara con `SumAboveAfter`?
