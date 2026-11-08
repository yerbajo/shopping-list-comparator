# Despliegue

La app es un binario Go que sirve la página y su API, y guarda la lista en un fichero JSON. No
necesita base de datos. Lo único que necesita de fuera es **el motor de catálogo** (el servidor de
[multisuper](https://github.com/yerbajo/multi-super)), que no vive en esta imagen.

## 1. Requisitos

- Docker 20.10+ (por `host-gateway`) y Docker Compose v2.
- Un multisuper sirviendo su API: `POST /api/search`. Compruébalo antes de nada:

  ```bash
  curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8099/api/caps   # 200
  ```

## 2. Con Docker Compose (lo recomendado)

```bash
git clone git@github.com:yerbajo/shopping-list-comparator.git
cd shopping-list-comparator
docker compose up -d --build
```

- La página queda en **http://127.0.0.1:8100**.
- El motor se alcanza por `host.docker.internal:8099`, que en Linux es el host real gracias a
  `extra_hosts: host.docker.internal:host-gateway`.
- La lista vive en el volumen `lista-data`, no en la imagen.

Comprobación rápida:

```bash
docker compose ps                      # Up (healthy)
curl -s http://127.0.0.1:8100/api/state | head -c 120
```

Variables que entiende el compose:

| Variable | Por defecto | Para qué |
|---|---|---|
| `SLC_MULTISUPER` | `http://host.docker.internal:8099` | URL del motor de catálogo |
| `SLC_BIND` | `127.0.0.1` | interfaz del host donde se publica el 8100 (`0.0.0.0` para la LAN) |
| `SLC_GAP` | `4s` | pausa entre búsquedas; súbela si tu lista es larga |

```bash
SLC_MULTISUPER=http://192.168.1.245:8099 SLC_BIND=0.0.0.0 docker compose up -d
```

## 3. Con `docker run`, si prefieres no usar compose

```bash
docker build -t shopping-list-comparator:local .

# el volumen que crea este compose (el prefijo es el nombre del directorio del clon)
VOLUME=$(docker inspect -f '{{range .Mounts}}{{.Name}}{{end}}' "$(docker compose ps -q lista)")

docker run -d --name lista \
  --restart unless-stopped \
  -p 127.0.0.1:8100:8100 \
  -v "$VOLUME":/data \
  --add-host host.docker.internal:host-gateway \
  shopping-list-comparator:local \
  -addr=0.0.0.0:8100 -multisuper=http://host.docker.internal:8099 -data=/data/list.json -gap=4s
```

Las banderas son las mismas que las del binario: `-addr`, `-multisuper`, `-data`, `-gap`, `-limit`.

## 4. La lista: copia y restauración

Compose crea un volumen con nombre `«directorio»_lista-data` (`shopping-list-comparator_lista-data` si
clonaste en `shopping-list-comparator/`). No lo escribas a mano: pregúntaselo al contenedor.

```bash
VOLUME=$(docker inspect -f '{{range .Mounts}}{{.Name}}{{end}}' "$(docker compose ps -q lista)")
echo "$VOLUME"     # shopping-list-comparator_lista-data

# copia (el --user evita dejar un fichero de root en tu directorio)
docker run --rm --user "$(id -u):$(id -g)" -v "$VOLUME":/data -v "$PWD":/backup alpine \
  tar czf /backup/lista-$(date +%F).tgz -C /data .

# restauración: para la app primero, o una petición tuya puede reescribir el fichero
BACKUP=lista-2026-11-08.tgz          # el que acabas de crear
docker compose stop lista
docker run --rm --user "$(id -u):$(id -g)" -v "$VOLUME":/data -v "$PWD":/backup alpine \
  sh -c "rm -f /data/list.json && tar xzf /backup/$BACKUP -C /data"
docker compose start lista
```

El fichero es JSON legible: `docker run --rm -v "$VOLUME":/data alpine cat /data/list.json`.

Si arrancaste con `docker run` (sin compose), `VOLUME` es el nombre que le diste:

```bash
VOLUME=lista-data
```

## 5. Motor en otra máquina

Si multisuper corre en otra máquina de la red, apunta la app allí y asegúrate de que ese multisuper
escucha en todas las interfaces (`-addr 0.0.0.0:8099`), no solo en loopback:

```bash
SLC_MULTISUPER=http://192.168.1.245:8099 docker compose up -d
```

## 6. Exponerla fuera del loopback

La app **no tiene autenticación**: es una lista personal. Si la publicas en la LAN, hazlo sabiéndolo
(`SLC_BIND=0.0.0.0`); si tiene que salir de la LAN, ponla detrás de un proxy con TLS y autenticación
(Caddy, nginx + basic auth) o de una VPN. Cualquiera que llegue al puerto puede añadir, borrar y
cambiar la lista.

## 7. Actualizar

```bash
git pull
docker compose up -d --build     # reconstruye la imagen y recrea el contenedor
```

El volumen no se toca: la lista sobrevive a la actualización.

## 8. Logs y salud

```bash
docker compose logs -f --tail=50
docker inspect --format '{{.State.Health.Status}}' "$(docker compose ps -q lista)"
```

El `HEALTHCHECK` pide `GET /api/state` cada 30 s; no consulta las cadenas, así que una cadena caída
no marca el contenedor como enfermo.

## 9. Cuando algo no funciona

| Síntoma | Causa | Arreglo |
|---|---|---|
| La cabecera dice «motor de catálogo sin conexión» | `SLC_MULTISUPER` mal, o el motor escucha solo en loopback | desde dentro del contenedor: `docker compose exec lista wget -qO- http://host.docker.internal:8099/api/caps` (o la URL que uses) |
| Todo aparece como «sin cuota» | ALCAMPO responde un WAF y CARREFOUR admite 3 peticiones/minuto | sube `SLC_GAP` (por ejemplo `12s`) y usa el botón de reintento |
| `permission denied` al escribir la lista | montaste un directorio del host y el usuario del contenedor es `10001` | usa el volumen con nombre, o `sudo chown -R 10001 /ruta/data` |
| `host.docker.internal` no resuelve | Docker antiguo o sin `host-gateway` | usa la IP del host en `SLC_MULTISUPER` |
| El contenedor aparece `unhealthy` y la app responde | cambiaste `-addr` y el `HEALTHCHECK` sigue pidiendo `127.0.0.1:8100` | deja `-addr=0.0.0.0:8100` y publica otro puerto en `ports:` |
| Perdiste la lista al recrear el contenedor | arrancaste con `docker run` sin `-v`: la imagen declara `VOLUME /data`, así que Docker creó un volumen anónimo | usa siempre `-v "$VOLUME":/data` |

## 10. Sin Docker (systemd, si algún día lo quieres)

```ini
[Unit]
Description=shopping-list-comparator
After=network-online.target

[Service]
User=alberto
ExecStart=/home/alberto/shopping-list-comparator/bin/web -addr=127.0.0.1:8100 \
  -multisuper=http://127.0.0.1:8099 -data=/home/alberto/.local/share/lista/list.json -gap=4s
Restart=on-failure

[Install]
WantedBy=default.target
```
