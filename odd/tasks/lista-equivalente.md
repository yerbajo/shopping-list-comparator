# lista-equivalente

Una lista de la compra única que se convierte en una cesta por supermercado, con el producto
equivalente localizado en cada cadena. App nueva en `github.com/yerbajo/shopping-list-comparator`,
distinta de multisuper: aquí la lista es el producto y la equivalencia entre cadenas es lo que se
construye, no lo que se evita.

## Specs

- **S1** — La app sirve para «generar listas de la compra en los distintos supermercados a partir de
  los productos que se añadan de cualquiera».
- **S2** — «Intenta localizar el mismo producto en los otros supermercados a partir del que se añada
  inicialmente»: el producto añadido es el ancla y la app propone su equivalente en cada cadena.
- **S3** — La equivalencia usa las marcas blancas de cada cadena, identificadas por su nombre: «un
  producto de la marca dia en dia o hacendado en mercadona o ifa en supermercado mas». DIA publica
  sub-marcas por categoría (`Dia Láctea`, `Dia Galleteca`, `Dia Arrozona`, `Diasol`, `Dia La Llama`,
  `Dia Super Paco`, `El Molino de Dia`, `Dia Vegecampo`, `Dia Al Punto`, `Dia Fidias`,
  `Dia Selección Mundial`, `Dia Mari Marinera`); Mercadona publica `Hacendado`, `Deliplus`,
  `Bosque Verde`, `Compy`, `Solcare`, `BabySmile`; MAS **no publica marca nunca** y sus productos IFA
  aparecen dentro del nombre (`IFA ELIGES`, `IFA UNNIA`, `ifaeliges`, `MASIASOL`).
- **S4** — «si añado un producto de dia a la cesta de la compra se generen multiples cestas de la
  compra añadiendo el mismo producto en cada supermercado»: la lista única se proyecta en una cesta
  por cadena, con su total y sus faltas.
- **S5** — «la equivalencia entre productos es lo que tienes que investigar o intentar generar»: el
  motor de equivalencia es la pieza central, con nivel de confianza declarado por producto y cadena.
- **S6** — «No quiero que sea un "clon" de multi-super, intenta darle una vuelta al diseño, estrcutura,
  colores, etc»: interfaz lista-primero, superficie fría neutra y acento cobalto, sin el papel cálido
  ni el verde pino de multisuper, sin hero de marketing y con la matriz lista × cadena como segunda
  vista.
- **S7** — La app se llama «shoping list comparator» y vive en
  `https://github.com/yerbajo/shopping-list-comparator`. El repo remoto está vacío: el primer push y
  la publicación son decisión del usuario; este trabajo deja el repo local con sus commits.
- **S8** — Arquitectura aceptada («me parece bien tu decisión, vamos a implementarlo»): el motor de
  catálogo se consume de multisuper por HTTP (`POST /api/search`), sin duplicar los siete clientes;
  marcas propias como dato (`brands.json`), no como código; puerta de dominio por categoría y nombre;
  ejes de variante tipados; persistencia en fichero JSON con escritura atómica; resolución en segundo
  plano con cola y presupuesto entre búsquedas; elección manual fijable por cadena; UI dirección A.
- **S9** — Evidencia de aceptación medida con los catálogos reales (10 items × 7 cadenas, 2026-11-08):
  la leche entera de marca blanca de 1 L cuesta 0,96 € en DIA, Mercadona y ALDI y 0,95 € en MAS;
  huevos M 12 ud y galletas maría dorada 800 g cuestan lo mismo en DIA, MAS y Mercadona. Cuatro falsos
  positivos deben quedar **rechazados** por el motor: «SILVERCREST® Cuecehuevos» para huevos,
  «LIVARNO HOME Escobobilla de váter» para papel higiénico, «Filetes de anchoa en aceite de girasol»
  para aceite de girasol y «SILVERCREST® Juego de moldes de silicona» para pan de molde. «Tomate frito
  estilo casero» **no** puede salir `equivalente` frente a «Tomate frito 0 % azúcares añadidos»
  (el +74 % falso del experimento).
- **S10** — Estados honestos por cadena: `exacta` (mismo EAN), `equivalente` (mismo producto, misma
  variante, mismo tamaño), `probable` (hay algo pero no es lo mismo o falta el tamaño), `no`
  (no encontrado) y cadena no disponible con su código (`quota`, `blocked`, `timeout`…). Nunca se
  inventa precio ni se declara «la más barata».

## Tasks

| ID | Specs | Ruta | Commit |
|---|---|---|---|
| T1 | S7, S8 | Bootstrap del módulo, `.gitignore`, documento y rama | `chore: bootstrap shopping-list-comparator` |
| T2 | S3, S5, S9, S10 | `internal/identity` (marcas, dominio, ejes, tamaño, niveles) + tests del corpus | `feat(identity): motor de equivalencia con marcas propias y ejes tipados` |
| T3 | S8 | `internal/catalog` cliente HTTP de multisuper + test con servidor falso | `feat(catalog): cliente del motor de catálogo de multisuper` |
| T4 | S4, S8 | `internal/store` lista persistente con escritura atómica + test | `feat(store): lista persistente en fichero JSON` |
| T5 | S2, S4, S8 | `internal/resolve` cola, ancla automática, candidatos + test | `feat(resolve): resolución en segundo plano con ancla y candidatos` |
| T6 | S1, S4, S6, S10 | `cmd/web` + `web/` API y UI lista-primero, cestas por cadena + test | `feat(web): lista única, raíl de precios y una cesta por cadena` |
| T7 | S7, S9 | README, verificación end-to-end contra multisuper | `docs: uso, alcance y límites medidos` |

## Log

- **L1** (usuario, verbatim): «partiendo de la app de la carpeta ~/multisuper que está en el repo
  https://github.com/yerbajo/multi-super vamos a hacer otra app llamada shoping list comparator que
  subiremos a https://github.com/yerbajo/shopping-list-comparator que se encargará de generar listas
  de la compra en los distintos supermercados a partir de los productos que se añadan de cualquiera.
  Intenta localizar el mismo producto en los otros supermercados a partir del que se añada
  inicialmente. Para ello es importante investigar e identificar las marcas blancas de las distintas
  cadenas para hacer esta equivalencia al añadir un producto de la marca dia en dia o hacendado en
  mercadona o ifa en supermercado mas. Mira si hay algo util en
  https://github.com/steel-dev/awesome-web-agents de cara a realizar el diseño de esta nueva web. No
  quiero que sea un "clon" de multi-super, intenta darle una vuelta al diseño, estrcutura, colores,
  etc»
- **L2** (usuario, verbatim): «la idea principal es que si añado un producto de dia a la cesta de la
  compra se generen multiples cestas de la compra añadiendo el mismo producto en cada supermercado .
  la equivalencia entre productos es lo que tienes que investigar o intentar generar. Continua con tu
  recomendación»
- **L3** (usuario, verbatim): «me parece bien tu decisión, vamos a implementarlo»
- **L4** (decisión, exploración): `awesome-web-agents` es un catálogo de agentes de navegador y
  scrapers; no aporta al diseño ni a la arquitectura. Único uso admitido, como plan B: navegador
  gestionado con sigilo o extracción por esquema para una cadena que cierre la API. Descartado un
  agente por consulta: más caro, más lento y no determinista.
- **L5** (evidencia, 2026-11-08): ejecución real de `multisuper find` sobre 10 items. 60 celdas
  cadena × item: ALCAMPO 10/10 bloqueada (WAF 202), CARREFOUR 8/10 con cuota agotada (3 req/min),
  LIDL 9/10 sin candidato por búsqueda de texto, ALDI 8/10 en `probable` por no publicar tamaño, MAS
  5 `equivalente`, MERCADONA 7 `equivalente`. 12 equivalencias con tamaño exacto y delta de precio
  mediano 0 % frente al ancla DIA.
- **L6** (decisión): el valor del producto no es «dónde es más barato» —los básicos cuestan lo mismo—
  sino la lista única proyectada en N cestas con su equivalente y sus faltas. Un titular de «la más
  barata» sería el error que los datos desmienten; S10 lo prohíbe.
- **L7** (defecto visto en multisuper, fuera de alcance aquí): la ayuda de `--only` en
  `cmd/multisuper/main.go:36` y `cmd/multisuper/basket.go:27` omite `mercadona`, que sí está
  registrado en `main.go:90`.
