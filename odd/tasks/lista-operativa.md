# lista-operativa

Del rediseño y de cuatro funciones que faltaban para que la lista sirva de verdad en la tienda: elegir
el producto al añadir, cantidades y secciones, una lista imprimible por supermercado y el precio por
unidad con aviso de formato.

## Specs

- **S1** — «no me convence mucho los colores»: identidad visual nueva, propia y distinta de multisuper,
  elegida por el usuario entre tres direcciones: **tinta y tomate** — papel casi blanco (`#FBFAF7`),
  tinta (`#15140F`), un solo acento rojo tomate (`#C8402A`), titulares en grotesca grande y cifras en
  monoespaciada; la firma es la regla (2 px de tinta sobre cada fila y filetes de 1 px entre cadenas).
  Nada de papel cálido ni verde pino.
- **S2** — «la funcionalidad» no convencía: al añadir, el usuario **elige el producto** que vio en la
  cadena, que pasa a ser la base. La app no decide la base por su cuenta.
- **S3** — **Cantidades** (1–99, con contador en la fila) y **secciones** (fresco, despensa, limpieza e
  higiene, otros), deducidas del producto y corregibles; los totales multiplican por la cantidad.
- **S4** — **Modo compra**: la lista de una cadena a pantalla completa, con casillas para marcar en la
  tienda, botón de imprimir, copiar al portapapeles para WhatsApp y enlace directo `/?tienda=dia`.
- **S5** — **€/unidad** publicado por cada cadena, visible en la celda y en la lista de la tienda, y el
  **aviso de formato** cuando el equivalente es otro tamaño («otro formato (1 L → 2,2 L)»).
- **S6** — Se mantiene la regla de la casa: **un binario, sin CDN y sin build**. Las fuentes se
  autoalojan embebidas (subset latin + latin-ext, 168 KB); la UI se construye con nodos y
  `textContent`; el nivel de equivalencia se declara con una palabra y nunca se inventa un precio.
- **S8** — «quiero que los supermercados donde se generarán las listas sea un conjunto seleccionable ya
  que es posible que no dispongas del mismo en tu ciudad o no te guste su calidad, etc. Asi que debe
  poder seleccionarse en que supermercado quieres la comparativa de cesta»: la selección de cadenas
  es un ajuste persistente que decide **qué se consulta, qué celdas hay y qué cestas se generan**. Un
  supermercado fuera de la selección no gasta su cuota, no aparece en la comparativa y no puede ser la
  base de un producto nuevo. Debe quedar al menos uno.
- **S7** — «investiga componentes para realizar webs atractivas»: se evaluaron las opciones del mercado
  y se descartó adoptar una librería. Motivo registrado en L2.

## Tasks

| ID | Specs | Ruta | Commit |
|---|---|---|---|
| T1 | S1, S6 | Fuentes autoalojadas, tokens, tipografía y reglas nuevas | `feat(ui): identidad «tinta y tomate»` |
| T2 | S2 | `POST /api/search` con caché y puerta de dominio + buscador con elección | `feat(ui): elegir el producto al añadir` |
| T3 | S3 | `quantity` y `section` en el modelo, `PATCH` y agrupación por pasillo | `feat(lista): cantidades y secciones` |
| T4 | S5 | €/unidad desde la cadena y aviso de formato en la celda | `feat(ui): €/unidad y aviso de formato` |
| T5 | S4 | Modo compra con casillas, impresión, copia y enlace directo | `feat(ui): lista para la tienda, imprimible y copiable` |
| T6 | S6, S7 | `POST /api/recompute`, pruebas nuevas, README y capturas | `docs: interfaz y funciones nuevas` |
| T7 | S8 | Selección persistente de cadenas en modelo, `POST /api/stores`, filtro de celdas y cestas, selector con logos | `feat(cestas): elegir en qué supermercados se compara` |

## Log

- **L1** (usuario, verbatim): «dale una vuelta. no me convence mucho los colores y la funcionalidad.
  investiga componentes para realizar webs atractivas»
- **L2** (investigación de componentes, 2026-11): lo que hoy se usa para webs atractivas es React —
  `shadcn/ui` con bloques de Aceternity, Magic UI, 21st.dev, React Bits. Adoptarlo obliga a meter React
  y un bundler en una app que hoy es un binario con tres ficheros embebidos. Alternativas sin build:
  **Web Awesome** (`@awesome.me/webawesome` 3.14, MIT, `dist-cdn` self-hosteable, componentes reales y
  accesibles) y **Pico CSS** (2.1, MIT, semántico, demasiado escaso para una rejilla de siete
  columnas); **daisyUI** 5.7 exige Tailwind, y Tailwind tiene CLI standalone (v4.3.5) pero añade un
  paso de build. Decisión del usuario: **vanilla y CSS a mano**.
- **L3** (usuario, respuestas del cuestionario): dirección **«Tinta y tomate»**; funciones marcadas:
  **las cuatro** (elegir producto al añadir, cantidades y secciones, modo compra imprimible, €/kg y
  aviso de formato); stack **vanilla, CSS a mano**.
- **L4** (evidencia, primera captura del buscador): con «huevos», ALDI ofrecía «Huevos de chocolate
  rellenos de crema de leche». El motor de equivalencia ya los rechazaba, pero el buscador los
  proponía como base. Arreglo: la puerta de dominio se aplica también a la consulta (`QueryDomain`),
  en el buscador y en `PickAnchor`, con prueba propia.
- **L5** (evidencia, capturas): las celdas mostraban «marca blanca de la cadena» en cada equivalencia;
  es la norma, no un aviso, así que se retiró del motivo visible (sigue sumando en la puntuación) y el
  motivo «marca distinta» se mantiene, que sí informa.
- **L6** (decisión): los motivos y las equivalencias se guardan con la lista, así que una mejora del
  motor no llega a lo ya escrito. Se añadió `POST /api/recompute` y su botón: vuelve a juzgar los
  productos guardados **sin gastar cuota**. Verificado sobre la lista real de cinco productos.
- **L7** (decisión): el buscador ordena los productos de cada cadena con la marca propia primero y el
  precio después; una estantería no se lee en el orden en que la cadena la devuelve.
- **L8** (usuario, verbatim): «quiero que los supermercados donde se generarán las listas sea un
  conjunto seleccionable ya que es posible que no dispongas del mismo en tu ciudad o no te guste su
  calidad, etc. Asi que debe poder seleccionarse en que supermercado quieres la comparativa de cesta».
  Decisión: la selección es un ajuste guardado en la lista (`chains` en el JSON, vacío = todas), el
  resolver sólo pregunta a las elegidas y el servidor sólo construye celdas y cestas para ellas. Al
  volver a activar una cadena se piden únicamente los productos que nunca la consultaron: no se
  re-resuelve lo que ya tenía respuesta. El selector usa los logos de S5/S8 y deja al menos una cadena.
- **L9** (defecto encontrado en la captura del selector): `aria-pressed` no se aplicaba porque el
  helper `el()` asignaba las claves `aria-*` como propiedades del nodo y no como atributos; el mismo
  fallo dejaba sin nombre accesible el botón de cada cesta. Corregido en un solo sitio (`setAttribute`
  para `data-*` y `aria-*`).
