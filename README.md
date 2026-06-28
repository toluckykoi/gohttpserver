# gohttpserver

- **Goal**: Build the most user-friendly HTTP file server.
- **Features**: Human-friendly UI, file upload support, automatic QR code generation for Apple and Android installers.

## Features

- [x] QR code generation
- [x] Breadcrumb path navigation
- [x] All assets bundled into a single binary
- [x] Different icons for different file types
- [x] Show / hide hidden files
- [x] Upload support (via Token or session authentication)
- [x] README.md preview
- [x] HTTP basic authentication
- [x] Partial refresh when navigating directories
- [x] Auto-merge path when a directory contains only one subdirectory
- [x] Directory archive download
- [x] Apple IPA auto-generates plist file, scannable by iPhone (requires HTTPS)
- [x] Plist proxy
- [x] CORS support
- [x] Offline download
- [x] Code file preview
- [x] File editing support
- [x] Global file search
- [x] Hide download and QR code buttons on small screens
- [x] Theme switching
- [x] Works behind Nginx
- [x] `.ghs.yml` configuration support (similar to `.htaccess`)
- [x] MD5 and SHA computation
- [x] Folder upload
- [x] Sort by size or modification time
- [x] Version info on home page
- [x] `/-/info/some.(apk|ipa)` API for detailed info
- [x] `/-/apk/info/some.apk` API for Android package info
- [x] Auto version tagging
- [x] Configuration file support
- [x] Quick copy download link
- [x] Display folder size
- [x] Create folder
- [x] Hold Alt to skip delete confirmation
- [x] Unzip zip files during upload (with extraction progress)

## Installation

Download a pre-built binary from [GitHub Releases](https://github.com/toluckykoi/gohttpserver/releases) or [Gitee Releases](https://gitee.com/toluckykoi/gohttpserver/releases).

## Usage

Listen on port 8000 on all interfaces with upload enabled:

```bash
gohttpserver -r ./ --port 8000 --upload
```

Enable file editing:

```bash
gohttpserver -r ./ --port 8000 --edit
```

Enable upload, delete, and edit all at once:

```bash
gohttpserver -r ./ --port 8000 --upload --delete --edit
```

Run `gohttpserver --help` to see more options.

## Docker (under development)

## Authentication

- Enable HTTP basic authentication:

  ```bash
  gohttpserver --auth-type http --auth-http username1:password1 --auth-http username2:password2
  ```

- Enable OpenID authentication:

  ```bash
  gohttpserver --auth-type openid --auth-openid https://login.example-hostname.com/openid/
  ```

- Enable OAuth2 proxy:

  ```bash
  gohttpserver --auth-type oauth2-proxy
  ```

  You can configure an HTTP reverse proxy to handle authentication. When using oauth2-proxy, the backend reads the user ID from the `X-Auth-Request-Email` header and the display name from `X-Auth-Request-Fullname`. Please configure the OAuth2 reverse proxy yourself. For more details see [oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy).

  The required headers are:

  | Header | Value |
  |--------|-------|
  | X-Auth-Request-Email | User ID |
  | X-Auth-Request-Fullname | Display name (URL-encoded) |
  | X-Auth-Request-User | User nickname (usually the email prefix) |

- Enable upload:

  ```bash
  gohttpserver --upload
  ```

- Enable delete and create-folder:

  ```bash
  gohttpserver --delete
  ```

- Enable file editing:

  ```bash
  gohttpserver --edit
  ```

## Advanced Usage

Add access rules per sub-directory via `.ghs.yml`. Example:

```yaml
---
upload: false
delete: false
edit: false
users:
- email: "codeskyblue@codeskyblue.com"
  delete: true
  upload: true
  edit: true
  token: 4567gf8asydhf293r23r
```

In this setup, with OpenID authentication enabled, the user "codeskyblue@codeskyblue.com" can delete / upload / edit inside any directory that contains a `.ghs.yml` file.

`token` is used for upload and edit. See [Upload via curl](#upload-via-curl).

For example, in the following directory structure, the user can delete / upload in `foo` but not in `bar`:

```
root -
  |-- foo
  |    |-- .ghs.yml
  |    `-- world.txt
  `-- bar
       `-- hello.txt
```

Use `--conf` to specify the config file name, see [the example config file](testdata/config.yml).

To mark which files are hidden / visible, add the following to `.ghs.yml`:

```yaml
accessTables:
- regex: block.file
  allow: false
- regex: visual.file
  allow: true
```

### IPA Plist Proxy

Used for HTTPS-enabled servers. The default is <https://plistproxy.herokuapp.com/plist>.

```bash
gohttpserver --plistproxy=https://someproxyhost.com/
```

Test that the proxy works:

```bash
http POST https://someproxyhost.com/plist < app.plist
{
	"key": "18f99211"
}
http GET https://someproxyhost.com/plist/18f99211
# displays the contents of app.plist
```

When gohttpserver is behind Nginx with HTTPS configured, the plistproxy is automatically disabled.

### Upload via curl

For example, upload a file named `foo.txt` to the `somedir` directory:

```bash
curl -F file=@foo.txt localhost:8000/somedir
{"destination":"somedir/foo.txt","success":true}
# Upload with a token
curl -F file=@foo.txt -F token=12312jlkjafs localhost:8000/somedir
{"destination":"somedir/foo.txt","success":true}

# Upload with a different filename
curl -F file=@foo.txt -F filename=hi.txt localhost:8000/somedir
{"destination":"somedir/hi.txt","success":true}
```

Upload a zip file and extract it (the zip is removed after successful extraction):

```bash
curl -F file=@pkg.zip -F unzip=true localhost:8000/somedir
{"success": true}
```

Note: filenames may not contain the characters `\/:*<>|`.

Upload an entire folder (preserves directory structure). Each file is sent with a `path` form field carrying its relative path; the server creates intermediate directories automatically:

```bash
# Upload a single file with a relative path, the server will create the MyFolder/ directory
curl -F file=@a.txt     -F path=MyFolder/a.txt     localhost:8000/somedir
curl -F file=@sub/b.txt -F path=MyFolder/sub/b.txt localhost:8000/somedir
# After upload: somedir/MyFolder/a.txt and somedir/MyFolder/sub/b.txt
```

### Edit files via curl

Edit a file's content (PUT request, requires `--edit` flag):

```bash
curl -X PUT -H "X-Token: 12312jlkjafs" -d "new file content" localhost:8000/somedir/foo.txt
{"destination":"somedir/foo.txt","success":true,"size":15}
```

Note: the `path` field may not contain `..` or absolute paths (directory traversal is rejected), and each path segment may not contain `\: * < > | "`. The frontend can generate these `path` values automatically via the folder-picker button (supported in Chrome / Edge / Firefox; Safari does not support it).

### Deploy behind Nginx

Recommended config, assuming gohttpserver listens on `127.0.0.1:8200`:

```
server {
  listen 80;
  server_name your-domain-name.com;

  location / {
    proxy_pass http://127.0.0.1:8200; # change to your address
    proxy_redirect off;
    proxy_set_header  Host    $host;
    proxy_set_header  X-Real-IP $remote_addr;
    proxy_set_header  X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header  X-Forwarded-Proto $scheme;

    client_max_body_size 0; # disable upload size limit
  }
}
```

When running behind Nginx, start gohttpserver with the `--xheaders` flag.

Reference: <http://nginx.org/en/docs/http/ngx_http_core_module.html#client_max_body_size>

gohttpserver also supports the `--prefix` flag, useful when the root path `/` is occupied by another service. Related issue: <https://github.com/codeskyblue/gohttpserver/issues/105>

Example:

```bash
# gohttpserver configuration
gohttpserver --prefix /foo --addr :8200 --xheaders
```

**Nginx configuration**:

```
server {
  listen 80;
  server_name your-domain-name.com;

  location /foo {
    proxy_pass http://127.0.0.1:8200; # change to your address
    proxy_redirect off;
    proxy_set_header  Host    $host;
    proxy_set_header  X-Real-IP $remote_addr;
    proxy_set_header  X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header  X-Forwarded-Proto $scheme;

    client_max_body_size 0; # disable upload size limit
  }
}
```

## FAQ

- [How to generate a self-signed certificate with openssl](http://stackoverflow.com/questions/10175812/how-to-create-a-self-signed-certificate-with-openssl)

### Search query format

Search follows Google-like universal format. Keywords are space-separated, and a `-` prefix excludes the keyword from the results.

1. `hello world` means both `hello` and `world` must be present.
2. `hello -world` means `hello` must be present and `world` must NOT be present.

## Developer Guide

Dependencies are managed via [govendor](https://github.com/kardianos/govendor).

1. First compile the frontend:

   ```shell
   cd frontend
   npm run build
   ```

2. Build a development binary. The **frontend/dist** directory must exist:

   ```shell
   go build
   ./gohttpserver
   ```

3. Build a single-binary release:

   ```shell
   # Build the project
   go build

   # Run
   ./gohttpserver.exe -r ./testdata --addr 127.0.0.1:8000 --upload --delete --edit
   ```

## Support

This project is forked from **[codeskyblue/gohttpserver](https://github.com/codeskyblue/gohttpserver)** (the original project is no longer maintained). Thanks to codeskyblue/gohttpserver for the open source support.

## License

This project is licensed under the [Apache-2.0](LICENSE) license.
