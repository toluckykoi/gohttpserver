export function pathJoin(parts: string[]): string {
  const result = parts
    .map((part, i) => {
      if (i === 0) {
        return part.replace(/\/+$/, '')
      }
      return part.replace(/^\/+|\/+$/g, '')
    })
    .filter(Boolean)
    .join('/')
  return result.startsWith('/') ? result : '/' + result
}

export function getExtension(filename: string): string {
  const ext = filename.split('.').pop()
  return ext ? ext.toLowerCase() : ''
}

export function getEncodePath(filepath: string, basePath: string = ''): string {
  const parts = filepath.split('/').map(v => encodeURIComponent(v))
  return pathJoin([basePath, ...parts])
}

export function checkPathNameLegal(name: string): boolean {
  const illegalChars = /[\\/:*<>|]/
  return !illegalChars.test(name)
}

export function parentDirectory(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/').slice(0, -1)
  return parts.join('/') || '/'
}

export function parseBreadcrumb(pathname: string): Array<{ name: string; path: string }> {
  const path = decodeURI(pathname || '/').split('?')[0]
  const parts = path.split('/')
  const breadcrumb: Array<{ name: string; path: string }> = []

  if (path === '/') {
    return breadcrumb
  }

  for (let i = 2; i <= parts.length; i++) {
    const name = parts[i - 1]
    if (!name) continue
    const currentPath = parts.slice(0, i).join('/')
    breadcrumb.push({
      name: name + (i === parts.length ? ' /' : ''),
      path: currentPath
    })
  }

  return breadcrumb
}

export function getQueryString(name: string, search: string = window.location.search): string | null {
  const urlParams = new URLSearchParams(search)
  return urlParams.get(name)
}