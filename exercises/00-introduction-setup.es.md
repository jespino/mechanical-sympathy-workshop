# Introducción y Configuración

> **¿Quieres saber más?** El término viene del piloto Jackie Stewart («no hace falta ser ingeniero para ser piloto de carreras, pero sí hay que tener simpatía mecánica») y Martin Thompson lo trajo al mundo del software. Su [blog Mechanical Sympathy](https://mechanical-sympathy.blogspot.com/) sigue siendo una lectura excelente.

**Simpatía mecánica** significa escribir software entendiendo la máquina sobre la que se ejecuta. No luchas contra ella, colaboras con ella. En Go, «la máquina» son en realidad tres capas, una encima de otra:

1. **El compilador**, que decide qué vive en el stack, qué se hace inline y qué comprobaciones se pueden eliminar
2. **El runtime**, que reserva memoria, ejecuta el garbage collector y planifica las goroutines sobre los hilos
3. **El hardware**, con sus cachés, líneas de caché, predictores de saltos y el coste de entrar en el kernel

![Las tres capas bajo tu código Go: compilador, runtime y hardware](images/00-layers.webp)

La mayor parte del tiempo Go te oculta todo esto, y eso es una virtud. Pero cuando un trozo de código es caliente, la diferencia entre un código que trabaja *con* estas capas y uno que trabaja *contra* ellas suele ser de 2x, 10x o más, normalmente sin cambiar el algoritmo.

## Cómo Funciona Este Workshop

El workshop va en **crescendo**. Empezamos con las herramientas para medir, seguimos con las optimizaciones más sencillas y vamos bajando hacia la máquina a medida que avanzamos:

| Parte | Módulos | Qué aprenderás |
|---|---|---|
| I · Primero, medir | 0–2 | Benchmarks que no mienten, `benchstat` y `pprof` |
| II · Asignaciones y el compilador | 3–9 | Pre-asignación, strings, escape analysis, inlining, interfaces, bounds checks |
| III · El GC y el runtime | 10–14 | Layout de structs y clases de tamaño, `sync.Pool`, presión sobre el GC, el scheduler, syscalls |
| IV · La memoria y el hardware | 15–17 | Cachés de CPU, contención y false sharing, predicción de saltos |
| V · Juntándolo todo | 18–19 | PGO y un proyecto final de optimización |

Todos los laboratorios siguen el mismo ciclo, que es también el que deberías usar en la vida real:

![El ciclo del workshop: medir, entender por qué, cambiar una cosa, volver a medir](images/00-loop.webp)

El paso central es el corazón de cada módulo. Miramos el mecanismo que hace lenta la versión lenta: normalmente en el código fuente de Go (el compilador, el runtime, la librería estándar), y en la Parte IV en el propio hardware, con contadores de rendimiento. Cuando entiendes el *por qué*, la solución suele ser evidente, y reconocerás el mismo patrón en código que no has visto nunca.

> **Una advertencia.** Todo en este workshop trata sobre código *caliente*. La mayoría del código no lo es. Por defecto gana la legibilidad, y solo la cambias por velocidad donde un perfil te diga que merece la pena. La Parte I existe precisamente para que nunca optimices a ciegas.

## Paso 1: Instala Go 1.27

Necesitas **Go 1.27 o superior**. Los internos que leemos son los de la versión 1.27, y algunos módulos dependen de funcionalidades recientes (`b.Loop`, maps con Swiss Tables, el GC Green Tea, la asignación especializada por tamaño).

Descárgalo de [go.dev/dl](https://go.dev/dl/) o usa tu gestor de paquetes, y comprueba:

```bash
go version
```

```
go version go1.27.3 linux/amd64
```

Cualquier versión 1.27.x vale. **No** necesitas compilar Go desde el código fuente: todo el workshop usa el toolchain estándar.

## Paso 2: Descarga el Código del Workshop

```bash
git clone https://github.com/jespino/mechanical-sympathy-workshop.git
cd mechanical-sympathy-workshop
```

## Paso 3: Instala las Herramientas

### benchstat (obligatorio)

`benchstat` convierte varias ejecuciones de un benchmark en estadísticas: medianas, intervalos de confianza y p-valores. Lo usaremos en todos los laboratorios.

```bash
go install golang.org/x/perf/cmd/benchstat@latest
benchstat -h
```

Asegúrate de que `$(go env GOPATH)/bin` está en tu `PATH`.

### Graphviz (opcional)

`go tool pprof -http=:` funciona sin él, pero la vista de grafo necesita `dot`:

```bash
# Debian/Ubuntu
sudo apt install graphviz
# macOS
brew install graphviz
```

### perf (opcional, solo Linux)

En la Parte IV usamos contadores hardware de rendimiento (fallos de caché, fallos de predicción de saltos) para *demostrar* lo que pasa dentro de la CPU. La herramienta es `perf`:

```bash
# Debian/Ubuntu
sudo apt install linux-tools-common linux-tools-$(uname -r)
# Fedora
sudo dnf install perf
```

Muchas distribuciones no dejan leer los contadores hardware a usuarios normales por defecto. Si `perf stat true` falla con un error de permisos, relaja la restricción mientras dure el workshop:

```bash
cat /proc/sys/kernel/perf_event_paranoid    # 3 o 4 significa «bloqueado»
sudo sysctl kernel.perf_event_paranoid=1
```

> ⚠️ **¿No usas Linux?** `perf` solo existe en Linux. En macOS o Windows (y dentro de muchas VMs y contenedores, que no exponen los contadores hardware), sáltate los pasos con `perf`. Todos los módulos funcionan también solo con benchmarks. Verás el efecto en `sec/op`, simplemente no verás el contador que lo explica. En macOS, la plantilla «CPU Counters» de Instruments puede mostrar datos parecidos si quieres profundizar.

## Paso 4: Verifica Tu Entorno

Desde la raíz del repositorio, comprueba que todos los laboratorios compilan y pasan sus tests, tanto en tu versión como en las soluciones de referencia:

```bash
make test-labs
```

Después, lanza tu primera comparación de benchmarks con la solución de referencia del laboratorio de pre-asignación:

```bash
make solution LAB=03
```

En unos segundos deberías ver una tabla de `benchstat` parecida a esta, con porcentajes muy negativos en la columna `vs base`:

```
              │    before     │                after                │
              │    sec/op     │    sec/op     vs base               │
Squares-32      28.684µ ± 30%   6.690µ ± 13%  -76.68% (p=0.002 n=6)
Evens-32        13.415µ ±  6%   7.069µ ±  7%  -47.31% (p=0.002 n=6)
CountWords-32    252.8µ ± 17%   152.6µ ± 10%  -39.63% (p=0.002 n=6)
```

Si funciona, ya estás listo. No te preocupes por entender la tabla todavía: de eso se encarga el siguiente módulo.

## Paso 5: Haz Que Tu Máquina Sea Aburrida

Los benchmarks miden tu código *más todo lo demás que esté haciendo tu máquina*. Para conseguir números estables:

- **Enchufa el portátil.** Con batería, las CPUs reducen su frecuencia de forma agresiva.
- **Cierra aplicaciones pesadas**: navegadores con muchas pestañas, indexadores del IDE, videollamadas, builds de Docker.
- **No toques la máquina** mientras se ejecuta un benchmark largo.
- **En Linux, plantéate usar el governor `performance`** mientras haces benchmarks: `sudo cpupower frequency-set -g performance`.
- **Mejor en bare metal.** Las VMs y los runners de CI comparten CPU con vecinos ruidosos.

Nunca tendrás cero ruido, y por eso siempre ejecutaremos los benchmarks varias veces y dejaremos que `benchstat` nos diga si una diferencia es real.
