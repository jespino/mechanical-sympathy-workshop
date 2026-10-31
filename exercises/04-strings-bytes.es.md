# Strings y Bytes

Los strings están por todas partes: líneas de log, cabeceras HTTP, claves de maps, JSON, nombres de métricas. En el módulo anterior vimos cómo hacer crecer un slice o un map esconde asignaciones. Los strings tienen un truco más: **nunca pueden cambiar**. Eso hace que sea seguro y barato compartirlos, pero también significa que cada conversión entre `string` y `[]byte` puede tener que copiar. En este módulo veremos cuándo ocurre esa copia, cuándo la elimina el compilador por ti y cómo evitar el caso que pilla a casi todo el mundo.

## Objetivos de Aprendizaje

Al terminar este módulo:

- Sabrás por qué los strings no pueden cambiar, y por qué eso hace que las conversiones `string` ↔ `[]byte` copien
- Sabrás qué conversiones son gratis y cuáles asignan memoria, medido, no adivinado
- Evitarás la asignación escondida en `m[string(b)]++`

## El Concepto: Bytes Inmutables

### Dos cabeceras

Un string es una pequeña cabecera con dos campos: un puntero a los bytes y una longitud. Un slice tiene esos dos más un tercero, la capacidad:

```go
type stringStruct struct {   type slice struct {
    str unsafe.Pointer           array unsafe.Pointer
    len int                      len   int
}                                cap   int
                             }
```

![La cabecera de un string tiene puntero y longitud; la de un slice añade capacidad. Convertir entre ellos copia los bytes](images/04-headers.webp)

La diferencia importante no es el campo extra. Es que **los bytes de un string no pueden cambiar nunca**. Eso es lo que hace que sea seguro compartirlos: sacar un substring (`s[3:10]`) o asignar un string a otra variable solo copia la cabecera, nunca los bytes, y un string puede ser clave de un map o pasarse entre goroutines sin locks.

La otra cara: un `[]byte` *sí* puede cambiar. Así que convertir entre ellos tiene que copiar los bytes. Si no, alguien podría modificar los bytes de un "string" que otros están usando.

### Cuándo la conversión es gratis

La copia solo hace falta si alguien pudiera ver cambiar los bytes. Cuando el compilador puede demostrar que nadie puede, se salta la copia y reutiliza los bytes que ya existen. Eso pasa cuando el valor convertido solo se usa un momento y después se descarta:

- **Búsquedas en maps**: `m[string(b)]`
- **Comparaciones**: `string(b) == "..."`
- **`switch string(b)`**
- **`for i, c := range []byte(s)`**
- **`[]byte(s)` que solo se lee, nunca se modifica**

La copia sigue haciendo falta cuando el resultado se guarda (en una variable que sobrevive a la función, se devuelve, o se mete en un map como clave nueva) o cuando se modifican los bytes.

Vamos a comprobarlo. El laboratorio incluye un pequeño programa que mide cada caso:

```bash
cd labs
go run ./04-strings-bytes/cmd/conversions
```

```
case                          ns/op     B/op  allocs/op
string(b), result kept        14.16       48          1
m[string(b)] lookup            8.11        0          0
m[string(b)]++                31.39       48          1
string(b) == "..."             4.18        0          0
switch string(b)               4.52        0          0
for range []byte(s)            8.46        0          0
[]byte(s), read only           1.89        0          0
[]byte(s), modified           13.11       48          1
```

(En mi máquina.) Fíjate en las dos filas del map: **`m[string(b)]` es gratis, pero `m[string(b)]++` no.** `++` escribe en el map y, si la clave es nueva, el map tiene que guardarla. Así que el compilador tiene que crear un string de verdad cada vez, aunque la clave ya esté. Esa es la trampa que arreglamos en el laboratorio.

### Construir strings

`s += x` en un bucle tiene el mismo problema que `append` sobre un slice que empieza vacío, pero peor: como un string no puede crecer en su sitio, cada `+=` crea un string nuevo y copia todo lo construido hasta entonces. El arreglo es la misma idea que en [Pre-asignación](03-preallocation.md): usa un `strings.Builder` (o un `bytes.Buffer`) y, si sabes el tamaño final, llama antes a `Grow(n)`.

## El Laboratorio

```
labs/04-strings-bytes/
  before.go           CountTokensBefore (línea base, no lo edites)
  after.go            tu copia: haz que esta sea rápida
  solution.go         solución de referencia (se compila con -tags solution)
  lab_test.go         test de corrección + benchmark
  cmd/conversions/    la tabla de conversiones de arriba
```

`CountTokensBefore` cuenta cuántas veces aparece cada token. Los tokens son slices `[]byte`, como lo serían si vinieran de un fichero o de una lectura de red:

```go
func CountTokensBefore(tokens [][]byte) map[string]int {
	counts := map[string]int{}
	for _, t := range tokens {
		counts[string(t)]++
	}
	return counts
}
```

El benchmark la ejecuta sobre 10.000 tokens que repiten solo 17 palabras distintas.

### Paso 1: Mide la línea base

```bash
make bench LAB=04
```

Mira `allocs/op`: unas 10.000, una por token, aunque solo hay 17 palabras distintas.

### Paso 2: Busca las pruebas

Pregúntale al profiler de memoria de dónde vienen las asignaciones. `-memprofilerate=1` registra todas las asignaciones en lugar de muestrear, y `-list` las muestra línea a línea:

```bash
cd labs
go test -run '^$' -bench 'CountTokens/impl=before' -memprofile mem.prof -memprofilerate 1 ./04-strings-bytes/
go tool pprof -sample_index=alloc_objects -list 'CountTokensBefore' mem.prof
```

```
         .          .      9:func CountTokensBefore(tokens [][]byte) map[string]int {
       681        681     10:	counts := map[string]int{}
         .          .     11:	for _, t := range tokens {
   2171031    2171031     12:		counts[string(t)]++
         .          .     13:	}
```

Casi todas las asignaciones vienen de `counts[string(t)]++`: un string nuevo por cada token, que se usa una vez para actualizar el map y después se tira.

### Paso 3: Arréglalo

Edita `labs/04-strings-bytes/after.go` para que actualizar un token que ya existe sea solo una búsqueda, que es gratis. Usa un `map[string]*int`: si `counters[string(t)]` devuelve un puntero, incrementa a través de él. Solo inserta una clave nueva (y asigna su string) la primera vez que aparece un token. Al final, conviértelo a un `map[string]int`.

`go test ./04-strings-bytes/` comprueba que los recuentos son los mismos.

### Paso 4: Compara

```bash
make bench LAB=04
```

Resultados con la solución de referencia (`make solution LAB=04`) en mi máquina:

```
               │    before    │               after                │
               │    sec/op    │   sec/op     vs base               │
CountTokens-32   233.52µ ± 6%   70.70µ ± 5%  -69.73% (p=0.002 n=6)

               │    before     │                after                │
               │     B/op      │     B/op      vs base               │
CountTokens-32   51.320Ki ± 0%   2.578Ki ± 0%  -94.98% (p=0.002 n=6)

               │    before     │               after               │
               │   allocs/op   │ allocs/op   vs base               │
CountTokens-32   10007.00 ± 0%   43.00 ± 0%  -99.57% (p=0.002 n=6)
```

## Entendiendo los Resultados

10.000 tokens, pero solo 17 palabras distintas. La versión before creaba una clave string por cada token. La versión after crea 17 claves y 17 contadores, y las 9 asignaciones restantes son el almacenamiento interno de los dos maps. Eso es 3,3 veces más rápido con un 99,6% menos de asignaciones.

La pasada extra que convierte el `map[string]*int` en `map[string]int` cuesta menos de lo que ahorramos. Si controlas la API, devuelve el `map[string]*int` (o un slice de recuentos) y sáltate también esa pasada.

> **Regla práctica:** una conversión solo cuesta algo cuando su resultado se *guarda* o se *modifica*. Las búsquedas, las comparaciones y las lecturas son gratis. Guardar y modificar, no.

## Puntos Clave

- Los strings son inmutables; convertir desde o hacia `[]byte` puede copiar.
- El compilador puede evitar copias en usos temporales de solo lectura.
- `m[string(b)]++` asigna memoria. Busca primero; actualiza mediante un puntero.
- Construye strings con `strings.Builder` y `Grow` en lugar de repetir `+=`.

## Para Ir Más Allá (opcional)

1. Añade tus propios casos a `cmd/conversions`. ¿Es gratis `string(b)` cuando se lo pasas a `strings.HasPrefix`? ¿Y `[]byte(s)` cuando se lo pasas a `bytes.IndexByte`?
2. Cambia `testTokens` en `lab_test.go` para que todos los tokens sean distintos. ¿Cuánto ahorra ahora la versión after, y por qué?
