/**
 * Previewable file formats and language detection.
 *
 * The preview system covers three families of files:
 *   - markdown:  can be rendered to HTML, with a source view as fallback
 *   - code:      raw text with syntax-aware highlighting
 *   - data:      raw text with structure-aware highlighting (json/yaml/toml)
 *
 * Anything else is treated as plain text.
 */

import { isImageFile, isVideoFile } from './fileIcon'

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
  /** Whether the file is previewable at all. */
  previewable: boolean
  /** Detected language, used to pick a highlighter. */
  language: PreviewLanguage
  /** Whether the file can be rendered to HTML (markdown only for now). */
  renderable: boolean
  /** Display label for the language (used in the UI badge). */
  label: string
}

const EXT_LANGUAGE: Record<string, PreviewLanguage> = {
  // markdown
  md: 'markdown',
  markdown: 'markdown',

  // data
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

  // markup
  xml: 'xml',
  html: 'xml',
  htm: 'xml',
  svg: 'xml',
  xhtml: 'xml',

  // code — shell
  sh: 'shell',
  bash: 'shell',
  zsh: 'shell',
  fish: 'shell',

  // code — sql
  sql: 'sql',

  // code — web
  js: 'javascript',
  mjs: 'javascript',
  cjs: 'javascript',
  jsx: 'javascript',
  ts: 'typescript',
  tsx: 'typescript',
  css: 'css',
  scss: 'css',
  less: 'css',

  // code — general
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

/** Filenames that have a known type but no extension (e.g. dotfiles). */
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
  // Exact-filename matches first (dotfiles, special files).
  if (filename in FILENAME_LANGUAGE) {
    const language = FILENAME_LANGUAGE[filename]
    return {
      previewable: true,
      language,
      renderable: language === 'markdown',
      label: LANG_LABELS[language]
    }
  }

  // Extension lookup.
  const dot = filename.lastIndexOf('.')
  if (dot === -1) {
    return { previewable: false, language: 'plain', renderable: false, label: '' }
  }
  const ext = filename.slice(dot + 1).toLowerCase()

  // Plain text extensions — always previewable, no special highlighting.
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

/** Convenience: can this file be previewed at all? */
export function isPreviewable(filename: string): boolean {
  return detectPreview(filename).previewable
}

// ────────────────────────────────────────────────────────────────────────────
// Click action — the answer to "what should happen when the user clicks the
// row?"  Used by the file list to give every file type a sensible default
// without forcing the user to use the action menu.
// ────────────────────────────────────────────────────────────────────────────

export type ClickAction =
  | { kind: 'navigate' }
  | { kind: 'preview-text' }
  | { kind: 'preview-image' }
  | { kind: 'play-video' }
  | { kind: 'download' }

/** Short user-facing description of the click action, for tooltips. */
export function clickActionLabel(action: ClickAction): string {
  switch (action.kind) {
    case 'navigate':     return 'Click to open'
    case 'preview-text': return 'Click to preview · Ctrl/Cmd+Click to download'
    case 'preview-image': return 'Click to preview · Ctrl/Cmd+Click to download'
    case 'play-video':   return 'Click to play · Ctrl/Cmd+Click to download'
    case 'download':     return 'Click to download'
  }
}

/**
 * Pick the right action for a row click.
 * `isDir` comes from the file item, not the filename, since the API
 * already classifies directories.
 */
export function getClickAction(
  filename: string,
  isDir: boolean
): ClickAction {
  if (isDir) return { kind: 'navigate' }
  if (isVideoFile(filename)) return { kind: 'play-video' }
  if (isImageFile(filename)) return { kind: 'preview-image' }
  if (isPreviewable(filename)) return { kind: 'preview-text' }
  return { kind: 'download' }
}
