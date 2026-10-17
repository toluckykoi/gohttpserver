import { isPreviewableImage, isVideoFile } from './fileIcon'

export type PreviewLanguage =
  | 'markdown'
  | 'json'
  | 'yaml'
  | 'xml'
  | 'toml'
  | 'ini'
  | 'shell'
  | 'sql'
  | 'javascript'
  | 'typescript'
  | 'python'
  | 'go'
  | 'rust'
  | 'java'
  | 'cpp'
  | 'css'
  | 'plain'

export interface PreviewableMeta {
  previewable: boolean
  language: PreviewLanguage
  renderable: boolean
  label: string
}

const EXT_LANGUAGE: Record<string, PreviewLanguage> = {
  md: 'markdown',
  markdown: 'markdown',

  json: 'json',
  jsonc: 'json',
  json5: 'json',
  yaml: 'yaml',
  yml: 'yaml',
  toml: 'toml',
  ini: 'ini',
  cfg: 'ini',
  conf: 'ini',
  env: 'ini',
  properties: 'ini',

  xml: 'xml',
  html: 'xml',
  htm: 'xml',
  svg: 'xml',
  xhtml: 'xml',

  sh: 'shell',
  bash: 'shell',
  zsh: 'shell',
  fish: 'shell',

  sql: 'sql',

  js: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  jsx: 'javascript',
  ts: 'typescript',
  tsx: 'typescript',
  css: 'css',
  scss: 'css',
  less: 'css',

  py: 'python',
  go: 'go',
  rs: 'rust',
  java: 'java',
  c: 'cpp',
  h: 'cpp',
  cpp: 'cpp',
  cc: 'cpp',
  cxx: 'cpp',
  hpp: 'cpp',
  rb: 'plain',
  php: 'plain'
}

const LANG_LABELS: Record<PreviewLanguage, string> = {
  markdown: 'Markdown',
  json: 'JSON',
  yaml: 'YAML',
  xml: 'XML',
  toml: 'TOML',
  ini: 'INI',
  shell: 'Shell',
  sql: 'SQL',
  javascript: 'JavaScript',
  typescript: 'TypeScript',
  python: 'Python',
  go: 'Go',
  rust: 'Rust',
  java: 'Java',
  cpp: 'C/C++',
  css: 'CSS',
  plain: 'Text'
}

/** 无扩展名但类型已知的点文件/特殊文件。 */
const FILENAME_LANGUAGE: Record<string, PreviewLanguage> = {
  Dockerfile: 'shell',
  Makefile: 'shell',
  '.bashrc': 'shell',
  '.zshrc': 'shell',
  '.profile': 'shell',
  '.gitignore': 'ini',
  '.gitattributes': 'ini',
  '.editorconfig': 'ini',
  '.npmrc': 'ini',
  '.eslintrc': 'json'
}

export function detectPreview(filename: string): PreviewableMeta {
  if (filename in FILENAME_LANGUAGE) {
    const language = FILENAME_LANGUAGE[filename]
    return {
      previewable: true,
      language,
      renderable: language === 'markdown',
      label: LANG_LABELS[language]
    }
  }

  const dot = filename.lastIndexOf('.')
  if (dot === -1) {
    return { previewable: false, language: 'plain', renderable: false, label: '' }
  }
  const ext = filename.slice(dot + 1).toLowerCase()

  if (ext === 'txt' || ext === 'text' || ext === 'log' || ext === 'csv') {
    return {
      previewable: true,
      language: 'plain',
      renderable: false,
      label: LANG_LABELS.plain
    }
  }

  const language = EXT_LANGUAGE[ext]
  if (language) {
    return {
      previewable: true,
      language,
      renderable: language === 'markdown',
      label: LANG_LABELS[language]
    }
  }

  return { previewable: false, language: 'plain', renderable: false, label: '' }
}

export function isPreviewable(filename: string): boolean {
  return detectPreview(filename).previewable
}

export type ClickAction =
  | { kind: 'navigate' }
  | { kind: 'preview-text' }
  | { kind: 'preview-image' }
  | { kind: 'play-video' }
  | { kind: 'download' }

/** 点击动作面向用户的简短描述，用于 tooltip。 */
export function clickActionLabel(action: ClickAction): string {
  switch (action.kind) {
    case 'navigate':     return 'Click to open'
    case 'preview-text': return 'Click to preview · Ctrl/Cmd+Click to download'
    case 'preview-image': return 'Click to preview · Ctrl/Cmd+Click to download'
    case 'play-video':   return 'Click to play · Ctrl/Cmd+Click to download'
    case 'download':     return 'Click to download'
  }
}

export function getClickAction(
  filename: string,
  isDir: boolean
): ClickAction {
  if (isDir) return { kind: 'navigate' }
  if (isVideoFile(filename)) return { kind: 'play-video' }
  if (isPreviewableImage(filename)) return { kind: 'preview-image' }
  if (isPreviewable(filename)) return { kind: 'preview-text' }
  return { kind: 'download' }
}
