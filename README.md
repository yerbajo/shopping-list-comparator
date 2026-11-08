# shopping-list-comparator

**Qué es.** Una lista de la compra única. Escribes un producto, eliges la cadena donde lo viste y la
app busca su equivalente en las demás —marcas blancas incluidas— y monta **una lista por
supermercado**, con cantidades, total y lo que allí falta.

**Qué no es.** No es un clon de [multisuper](https://github.com/yerbajo/multi-super) y no declara
«el más barato». La pasada medida con los catálogos reales dio la misma marca blanca al mismo precio
en varias cadenas (leche entera 1 L a 0,96 € en DIA, Mercadona, ALDI y 0,95 € en MAS; huevos M 12 ud
y galletas maría dorada 800 g a idéntico precio en DIA, MAS y Mercadona), así que el valor está en la
lista resuelta, no en un ranking.

![La lista: una fila por producto y una celda por cadena con su precio, su €/L y su nivel](docs/web/desktop.png)

## Arrancarlo

Con Docker, en un comando (detalles y copias de seguridad en [docs/despliegue.md](docs/despliegue.md)):

```bash
docker compose up -d --build      # http://127.0.0.1:8100, motor en el host:8099
```

La imagen no lleva el motor de catálogo: apunta a un multisuper con
`SLC_MULTISUPER=http://host:8099`. La lista vive en el volumen `lista-data`.

Sin Docker, el catálogo lo sirve multisuper; esta app no duplica sus siete clientes.

```bash
# 1. el motor de catálogo, en otra terminal (repo multisuper)
go run ./cmd/web -addr 127.0.0.1:8099

# 2. la lista
go build -o bin/ ./...
bin/web -addr 127.0.0.1:8100 -multisuper http://127.0.0.1:8099 -data data/list.json
```

| Bandera | Por defecto | Qué hace |
|---|---|---|
| `-addr` | `127.0.0.1:8100` | dónde sirve la web |
| `-multisuper` | `http://127.0.0.1:8099` | motor de catálogo (`POST /api/search`) |
| `-data` | `data/list.json` | fichero de la lista |
| `-gap` | `4s` | pausa entre búsquedas; súbela si tu lista es larga |
| `-limit` | `20` | productos pedidos a cada cadena por item |

## Qué supermercados entran

Arriba de la lista hay **un selector con las siete cadenas**. Solo se consultan las que marques, solo
para ellas hay celdas y solo para ellas se genera cesta: un supermercado que no está en tu ciudad —o
que no te convence— no gasta su cuota ni ensucia la comparativa. La selección se guarda con la lista
(fichero y volumen), así que sobrevive a reinicios y actualizaciones. Al volver a activar una cadena,
la app pide solo los productos que nunca la consultaron.

## Cómo se usa

1. **Buscar** un producto. La app pregunta a las siete cadenas y te enseña lo que cada una tiene, con
   su precio y su €/kg o €/L. Un producto de otro dominio no aparece: escribir «huevos» no ofrece
   huevos de chocolate.
2. **Elegir** el producto que viste. Ese es la base: su variante y su tamaño mandan en la equivalencia.
3. La app resuelve **su equivalente en cada cadena** con su nivel de confianza y añade el producto a
   su cesta. Desde la fila puedes cambiar la base o fijar otro producto para una cadena concreta.
4. **Cantidades y secciones.** Cada fila lleva su contador (1–99) y su sección (fresco, despensa,
   limpieza u otros; se deduce del producto y se puede cambiar). Los totales multiplican.
5. **Ir a la tienda.** Pulsa una cadena y se abre **su lista**, con casillas para marcar, botón de
   imprimir y de copiar para WhatsApp. El enlace es compartible: `/?tienda=dia`.

![Tus supermercados: seis de siete seleccionados, LIDL fuera](docs/web/supermercados.png)

| Buscador | Lista para la tienda |
|---|---|
| ![Buscador con los productos de cada cadena](docs/web/buscar.png) | ![Lista de DIA con casillas](docs/web/tienda.png) |

En móvil la rejilla pasa a dos columnas:

![La misma lista en el móvil](docs/web/movil.png)

## Cómo se decide que dos productos son el mismo

`internal/identity` aplica una escalera y **declara el resultado**, nunca lo esconde:

| Nivel | Significado | ¿Entra en el total de la cesta? |
|---|---|---|
| `exacta` | mismo código de barras (hoy solo Carrefour publica EAN) | sí |
| `equivalente` | mismo producto, misma variante y mismo tamaño | sí |
| `probable` | otra variante, otro formato o tamaño no publicado | **no** |
| `elegida` | la eligió una persona desde la fila | sí |
| `no` | el catálogo se buscó y no está | — |
| `unavailable` | la cadena no respondió; se muestra su código (`quota`, `blocked`, `timeout`…) | — |

La escalera, en orden: **EAN** → **dominio** (la puerta que rechaza menaje, mascotas, conservas y
dulces: es la que impide que un «cuecehuevos» sea un huevo) → **las palabras que pediste** (cobertura
≥ 60 % y el sustantivo de cabeza entre las tres primeras palabras del nombre) → **ejes de variante
tipados** (línea, grasa, grano, elaboración, estilo, grado, crianza, especie, tipo) → **tamaño**
normalizado a g/ml/ud, con 6 % de holgura.

Un eje tipado evita las contradicciones falsas que produjo el primer prototipo: «arroz extra» y
«arroz redondo» son el mismo producto (grado frente a grano), mientras que «tomate frito 0 % azúcares»
y «tomate frito estilo casero» no lo son —ese falso positivo valía un +74 % de precio.

El **€/unidad** lo publica cada cadena y se muestra tal cual aparece: es lo que permite comparar
formatos (2,2 L a 2,75 € son 1,25 €/L, no «más barato» que 1 L a 0,96 €).

## Marcas blancas: dato, no código

`internal/identity/brands.json` es el léxico. Cada cadena admite tres reglas independientes porque las
cadenas no publican la marca igual:

```json
"dia": {
  "prefix": ["dia"],            // el campo brand empieza por "dia": Dia Láctea, Diasol, Dia Vegecampo…
  "brand":  ["dia"],            // el campo brand contiene
  "name":   ["el molino de dia", "dia galleteca"]
},
"mas": {
  "name": ["ifa eliges", "ifa unnia", "ifaeliges", "ifaunnia", "masiasol"]
}
```

- **DIA** nombra una sub-marca por categoría (`Dia Láctea`, `Dia Galleteca`, `Dia Arrozona`,
  `Dia Vegecampo`, `Dia Al Punto`, `Dia Super Paco`, `Diasol`, `Dia La Llama`, `El Molino de Dia`…),
  así que la regla es un prefijo, no una lista cerrada.
- **Mercadona** publica `Hacendado`, `Deliplus`, `Bosque Verde`, `Compy`, `Solcare`, `BabySmile`.
- **MAS no publica marca nunca** (`brand` es `null` en todos sus productos): sus IFA aparecen dentro del
  nombre, a veces sin espacio (`ifaeliges`). Cuando el nombre tampoco la delata, la app dice «marca no
  publicada» en vez de inventar una paridad.
- **ALDI** publica 65+ marcas propias con `®`; **LIDL** las suyas (`Milbona`, `Sondey`, `Freeway`…);
  **Alcampo** usa `PRODUCTO ALCAMPO` y `AUCHAN` en el nombre.

Para añadir una marca: una línea en `brands.json` y un caso en `TestOwnBrand`. Nada más.

## Límites medidos (2026-11-08, 10 items × 7 cadenas)

| Cadena | Qué se encontró | Consecuencia en la app |
|---|---|---|
| DIA | 100 % con marca publicada y tamaño en el nombre | la mejor fuente de ancla |
| Mercadona | marca y `size` publicados | equivalencias exactas de tamaño |
| MAS | marca `null`; tamaño en el nombre (`p4x200g`, `1k`) | marca blanca detectada por léxico |
| ALDI | **no publica tamaño** | casi todo queda en `probable` |
| LIDL | su búsqueda por texto devuelve menaje para términos de comida | 9 de 10 items sin candidato; necesita recuperación por categoría |
| CARREFOUR | 3 peticiones/minuto (Superfudis); sí publica EAN y tamaño | aparece como `quota` en listas largas; botón de reintento |
| ALCAMPO | WAF de AWS (HTTP 202) intermitente | aparece como `quota`; reintento |

Por eso la resolución va en **segundo plano, de una en una, con pausa** (`-gap`): diez items son diez
fan-outs de siete cadenas, y sin cola la lista agota sus propias cuotas. «Recalcular equivalencias»
vuelve a juzgar los productos ya guardados **sin preguntar a ninguna cadena**.

## Interfaz

Cada cesta lleva **el logo oficial de su cadena** (los siete vectores viven en `web/logos.svg`,
recortados a su propio dibujo y embebidos en el binario; se reutilizan de multisuper, donde ya estaban
dibujados en un recuadro común de 106 × 30). Las marcas son de sus titulares y se usan solo para
identificar la cadena.

Identidad propia, no la de multisuper: papel casi blanco, tinta negra, un solo acento rojo tomate,
grotesca grande para los titulares y monoespaciada para las cifras. La firma es la regla: 2 px de
tinta encima de cada fila y filetes de 1 px separando las cadenas, como una tabla de periódico. El
nivel se dice con una palabra; el color solo tiñe esa palabra y el punto de cada cadena.

Sin CDN y sin build: las dos fuentes (Inter Tight variable y JetBrains Mono, subset latin + latin-ext)
están en `web/fonts/` y se embeben en el binario, 168 KB en total. Las animaciones son transiciones
CSS de 100–150 ms sobre propiedades concretas, `scale(0.96)` al pulsar y nada cuando el sistema pide
movimiento reducido.

## Estructura

```
cmd/web/              servidor HTTP, API y la página
internal/identity/    la escalera de equivalencia + brands.json + los tests del corpus real
internal/catalog/     cliente de POST /api/search (multisuper), céntimos y €/unidad
internal/store/       la lista en un JSON con escritura atómica
internal/resolve/     cola en segundo plano, ancla automática, alternativas
web/                  index.html, styles.css, app.js y las fuentes (todo embebido)
Dockerfile            imagen de dos etapas, usuario sin privilegios, lista en /data
docker-compose.yml    la lista con su volumen y el motor del host
docs/despliegue.md    Docker, copias, LAN, actualización y problemas
odd/tasks/            las specs del trabajo, con las peticiones literales y la evidencia
```

La lista es un fichero local (`data/list.json`); no hay cuentas, ni servidor de terceros, ni
histórico de precios.

## API

| Método y ruta | Qué hace |
|---|---|
| `GET /api/state` | la lista entera: items, celdas por cadena, cestas por supermercado |
| `POST /api/search` | `{"query":"huevos"}`: lo que cada cadena tiene, para elegir (caché de 30 s) |
| `POST /api/items` | `{"query":"leche entera","anchor":{…}}`: añade con ese producto como base |
| `PATCH /api/items/{id}` | `{"quantity":2}` o `{"section":"despensa"}` |
| `DELETE /api/items/{id}` | quita un item |
| `POST /api/items/{id}/pick` | `{"store":"aldi","id":"…"}`: fija ese producto en esa cadena |
| `POST /api/items/{id}/anchor` | `{"store":"dia","id":"…"}`: cambia la base y recalcula sin gastar cuota |
| `POST /api/items/{id}/retry` · `POST /api/retry` | reintenta lo que falló por cuota o red |
| `POST /api/recompute` | vuelve a juzgar lo guardado con las reglas actuales, sin red |
| `POST /api/stores` | `{"chains":["dia","mas"]}`: qué supermercados entran en la comparativa |

Enlaces directos: `/?tienda=dia` abre la lista de esa cadena y `/?buscar=huevos` abre el buscador.

## Tests

```bash
go test ./...
```

Los casos de `internal/identity` son nombres y precios reales medidos con `multisuper find`. Cuatro
falsos positivos viven ahí como casos negativos permanentes: `SILVERCREST® Cuecehuevos` (huevos),
`LIVARNO HOME Escobobilla de váter` (papel higiénico), `Filetes de anchoa en aceite de girasol`
(aceite de girasol) y `Juego de moldes de silicona` (pan de molde), más el +74 % de «tomate frito
estilo casero», los huevos de codorniz frente a los de gallina y los huevos de chocolate en el
buscador. `cmd/web/deploy_test.go` comprueba además que el despliegue documentado sigue siendo el que
este repo produce.
