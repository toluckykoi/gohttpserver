export type FileKind =
  | 'folder' | 'go' | 'code' | 'pdf' | 'zip' | 'audio'
  | 'image' | 'video' | 'apple' | 'windows' | 'apk' | 'default'

const pathMap: Record<FileKind, string[]> = {
  folder: [
    'M13.821 6.5h5.929a2.25 2.25 0 0 1 2.229 1.938l.016.158.005.154v9a2.25 2.25 0 0 1-2.096 2.245L19.75 20H4.25a2.25 2.25 0 0 1-2.245-2.096L2 17.75v-7.251l6.207.001.196-.009a2.25 2.25 0 0 0 1.088-.393l.156-.12L13.821 6.5ZM8.207 4c.46 0 .908.141 1.284.402l.156.12 2.103 1.751-3.063 2.553-.085.061a.75.75 0 0 1-.29.106L8.206 9 2 8.999V6.25a2.25 2.25 0 0 1 2.096-2.245L4.25 4h3.957Z'
  ],
  // Fluent: document-24-filled
  default: [
    'M12 2v6a2 2 0 0 0 2 2h6v10a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h6Z',
    'M13.5 2.5V8a.5.5 0 0 0 .5.5h5.5l-6-6Z'
  ],
  // MDI: language-go
  go: [
    'M2.64,10.33L2.62,10.27L2.84,10L2.96,9.92H6.8L6.83,10L6.65,10.26L6.54,10.32L2.64,10.33M1.03,11.31L1,11.26L1.22,10.97L1.34,10.91H6.24L6.29,11L6.21,11.24L6.11,11.31H1.03M3.63,12.3L3.59,12.24L3.75,11.96L3.85,11.9H6L6.07,11.97L6.05,12.22L5.97,12.3H3.63M14.78,10.14L13,10.61C12.81,10.65 12.8,10.66 12.66,10.5C12.5,10.32 12.39,10.21 12.16,10.1C11.5,9.76 10.83,9.86 10.22,10.25C9.5,10.73 9.11,11.42 9.12,12.3C9.13,13.16 9.72,13.87 10.57,14C11.3,14.09 11.91,13.83 12.4,13.28L12.69,12.89H10.62C10.4,12.89 10.35,12.75 10.42,12.57L10.97,11.39C11,11.33 11.08,11.22 11.24,11.22H14.68C14.83,10.72 15.09,10.26 15.43,9.81C16.21,8.78 17.16,8.24 18.43,8C19.5,7.82 20.56,7.93 21.5,8.57C22.34,9.15 22.87,9.93 23,10.96C23.19,12.41 22.76,13.59 21.76,14.61C21.05,15.33 20.18,15.78 19.19,16L18.33,16.08C17.35,16.06 16.46,15.78 15.71,15.13C15.19,14.68 14.83,14.14 14.65,13.5C14.5,13.74 14.38,13.97 14.21,14.2C13.44,15.22 12.43,15.85 11.15,16C10.1,16.16 9.12,15.95 8.26,15.31C7.47,14.71 7,13.91 6.9,12.92C6.76,11.75 7.1,10.7 7.81,9.78C8.57,8.78 9.58,8.15 10.82,7.92C11.82,7.74 12.79,7.86 13.66,8.44C14.23,8.82 14.63,9.34 14.9,9.96C14.94,10.05 14.9,10.11 14.78,10.14M20.89,11.74L20.86,11.38C20.67,10.32 19.69,9.72 18.67,9.95C17.66,10.17 17,10.8 16.79,11.81C16.6,12.65 17,13.5 17.77,13.84C18.36,14.1 18.96,14.06 19.53,13.78C20.37,13.35 20.84,12.66 20.89,11.74Z'
  ],
  // MDI: file-code
  code: [
    'M13,9H18.5L13,3.5V9M6,2H14L20,8V20A2,2 0 0,1 18,22H6C4.89,22 4,21.1 4,20V4C4,2.89 4.89,2 6,2M6.12,15.5L9.86,19.24L11.28,17.83L8.95,15.5L11.28,13.17L9.86,11.76L6.12,15.5M17.28,15.5L13.54,11.76L12.12,13.17L14.45,15.5L12.12,17.83L13.54,19.24L17.28,15.5Z'
  ],
  // MDI: file-pdf-box
  pdf: [
    'M19 3H5C3.9 3 3 3.9 3 5V19C3 20.1 3.9 21 5 21H19C20.1 21 21 20.1 21 19V5C21 3.9 20.1 3 19 3M9.5 11.5C9.5 12.3 8.8 13 8 13H7V15H5.5V9H8C8.8 9 9.5 9.7 9.5 10.5V11.5M14.5 13.5C14.5 14.3 13.8 15 13 15H10.5V9H13C13.8 9 14.5 9.7 14.5 10.5V13.5M18.5 10.5H17V11.5H18.5V13H17V15H15.5V9H18.5V10.5M12 10.5H13V13.5H12V10.5M7 10.5H8V11.5H7V10.5Z'
  ],
  // MDI: zip-box
  zip: [
    'M14,17H12V15H10V13H12V15H14M14,9H12V11H14V13H12V11H10V9H12V7H10V5H12V7H14M19,3H5C3.89,3 3,3.89 3,5V19A2,2 0 0,0 5,21H19A2,2 0 0,0 21,19V5C21,3.89 20.1,3 19,3Z'
  ],
  // MDI: file-music
  audio: [
    'M14,2H6A2,2 0 0,0 4,4V20A2,2 0 0,0 6,22H18A2,2 0 0,0 20,20V8L14,2M13,13H11V18A2,2 0 0,1 9,20A2,2 0 0,1 7,18A2,2 0 0,1 9,16C9.4,16 9.7,16.1 10,16.3V11H13V13M13,9V3.5L18.5,9H13Z'
  ],
  // MDI: file-image
  image: [
    'M13,9H18.5L13,3.5V9M6,2H14L20,8V20A2,2 0 0,1 18,22H6C4.89,22 4,21.1 4,20V4C4,2.89 4.89,2 6,2M6,20H15L18,20V12L14,16L12,14L6,20M8,9A2,2 0 0,0 6,11A2,2 0 0,0 8,13A2,2 0 0,0 10,11A2,2 0 0,0 8,9Z'
  ],
  // MDI: file-video
  video: [
    'M13,9H18.5L13,3.5V9M6,2H14L20,8V20A2,2 0 0,1 18,22H6C4.89,22 4,21.1 4,20V4C4,2.89 4.89,2 6,2M17,19V13L14,15.2V13H7V19H14V16.8L17,19Z'
  ],
  // MDI: apple
  apple: [
    'M18.71,19.5C17.88,20.74 17,21.95 15.66,21.97C14.32,22 13.89,21.18 12.37,21.18C10.84,21.18 10.37,21.95 9.1,22C7.79,22.05 6.8,20.68 5.96,19.47C4.25,17 2.94,12.45 4.7,9.39C5.57,7.87 7.13,6.91 8.82,6.88C10.1,6.86 11.32,7.75 12.11,7.75C12.89,7.75 14.37,6.68 15.92,6.84C16.57,6.87 18.39,7.1 19.56,8.82C19.47,8.88 17.39,10.1 17.41,12.63C17.44,15.65 20.06,16.66 20.09,16.67C20.06,16.74 19.67,18.11 18.71,19.5M13,3.5C13.73,2.67 14.94,2.04 15.94,2C16.07,3.17 15.6,4.35 14.9,5.19C14.21,6.04 13.07,6.7 11.95,6.61C11.8,5.46 12.36,4.26 13,3.5Z'
  ],
  // MDI: microsoft-windows
  windows: [
    'M3,12V6.75L9,5.43V11.91L3,12M20,3V11.75L10,11.9V5.21L20,3M3,13L9,13.09V19.9L3,18.75V13M20,13.25V22L10,20.09V13.1L20,13.25Z'
  ],
  // MDI: android
  apk: [
    'M16.61 15.15C16.15 15.15 15.77 14.78 15.77 14.32S16.15 13.5 16.61 13.5H16.61C17.07 13.5 17.45 13.86 17.45 14.32C17.45 14.78 17.07 15.15 16.61 15.15M7.41 15.15C6.95 15.15 6.57 14.78 6.57 14.32C6.57 13.86 6.95 13.5 7.41 13.5H7.41C7.87 13.5 8.24 13.86 8.24 14.32C8.24 14.78 7.87 15.15 7.41 15.15M16.91 10.14L18.58 7.26C18.67 7.09 18.61 6.88 18.45 6.79C18.28 6.69 18.07 6.75 18 6.92L16.29 9.83C14.95 9.22 13.5 8.9 12 8.91C10.47 8.91 9 9.24 7.73 9.82L6.04 6.91C5.95 6.74 5.74 6.68 5.57 6.78C5.4 6.87 5.35 7.08 5.44 7.25L7.1 10.13C4.25 11.69 2.29 14.58 2 18H22C21.72 14.59 19.77 11.7 16.91 10.14H16.91Z'
  ]
}

// MDI: git —— .git 目录特判，不属于 FileKind 体系。
const GIT_PATHS = [
  'M2.6,10.59L8.38,4.8L10.07,6.5C9.83,7.35 10.22,8.28 11,8.73V14.27C10.4,14.61 10,15.26 10,16A2,2 0 0,0 12,18A2,2 0 0,0 14,16C14,15.26 13.6,14.61 13,14.27V9.41L15.07,11.5C15,11.65 15,11.82 15,12A2,2 0 0,0 17,14A2,2 0 0,0 19,12A2,2 0 0,0 17,10C16.82,10 16.65,10 16.5,10.07L13.93,7.5C14.19,6.57 13.71,5.55 12.78,5.16C12.35,5 11.9,4.96 11.5,5.07L9.8,3.38L10.59,2.6C11.37,1.81 12.63,1.81 13.41,2.6L21.4,10.59C22.19,11.37 22.19,12.63 21.4,13.41L13.41,21.4C12.63,22.19 11.37,22.19 10.59,21.4L2.6,13.41C1.81,12.63 1.81,11.37 2.6,10.59Z'
]

// 各类型柔和强调色；低饱和度以兼顾明暗主题。
const colorMap: Record<FileKind, string> = {
  folder: '#BA7517',
  go: '#00ADD8',      // Go 官方品牌色
  image: '#7F77DD',
  video: '#D4537E',
  audio: '#1D9E75',
  code: '#378ADD',
  pdf: '#E24B4A',
  zip: '#D85A30',
  apple: '#888780',
  windows: '#378ADD',
  apk: '#639922',
  default: '#888780'
}

export function getFileKind(filename: string, type: 'file' | 'dir'): FileKind {
  if (type === 'dir') {
    return 'folder'
  }

  const ext = filename.split('.').pop()?.toLowerCase() || ''

  switch (ext) {
    case 'go':
      return 'go'
    case 'py':
    case 'js':
    case 'ts':
    case 'java':
    case 'c':
    case 'cpp':
    case 'h':
    case 'hpp':
    case 'rs':
    case 'rb':
    case 'php':
    case 'html':
    case 'css':
    case 'scss':
    case 'json':
    case 'yaml':
    case 'yml':
    case 'xml':
      return 'code'
    case 'pdf':
      return 'pdf'
    case 'zip':
    case 'rar':
    case '7z':
    case 'tar':
    case 'gz':
      return 'zip'
    case 'mp3':
    case 'wav':
    case 'flac':
    case 'aac':
    case 'ogg':
    case 'm4a':
      return 'audio'
    case 'jpg':
    case 'jpeg':
    case 'png':
    case 'gif':
    case 'bmp':
    case 'svg':
    case 'webp':
    case 'tiff':
      return 'image'
    case 'mp4':
    case 'webm':
    case 'avi':
    case 'mkv':
    case 'mov':
    case 'wmv':
      return 'video'
    case 'ipa':
    case 'dmg':
      return 'apple'
    case 'apk':
      return 'apk'
    case 'exe':
    case 'msi':
      return 'windows'
    default:
      return 'default'
  }
}

/** 文件图标的 SVG path 数据（24x24 viewBox，fill=currentColor）。 */
export function getFileIconPaths(filename: string, type: 'file' | 'dir'): string[] {
  if (type === 'dir' && filename === '.git') {
    return GIT_PATHS
  }
  return pathMap[getFileKind(filename, type)]
}

/** 图标底色块按类型取用的强调色。 */
export function getFileIconColor(filename: string, type: 'file' | 'dir'): string {
  return colorMap[getFileKind(filename, type)]
}

export function shouldHaveQrcode(filename: string): boolean {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  return ['apk', 'ipa'].includes(ext)
}

export function isVideoFile(filename: string): boolean {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  const videoExtensions = ['mp4', 'webm', 'ogg', 'mov', 'avi', 'mkv']
  return videoExtensions.includes(ext)
}

export function isImageFile(filename: string): boolean {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  return [
    'jpg', 'jpeg', 'png', 'gif', 'bmp', 'svg', 'webp',
    'avif', 'ico', 'tif', 'tiff', 'heic', 'heif'
  ].includes(ext)
}

/** `isImageFile` 中浏览器可解码的子集：排除多数桌面浏览器渲染不了的 TIFF/HEIC。 */
export function isPreviewableImage(filename: string): boolean {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  return [
    'jpg', 'jpeg', 'png', 'gif', 'bmp', 'svg', 'webp',
    'avif', 'ico'
  ].includes(ext)
}

export function isAudioFile(filename: string): boolean {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  return [
    'mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a', 'opus', 'wma'
  ].includes(ext)
}

export function isPdfFile(filename: string): boolean {
  return filename.split('.').pop()?.toLowerCase() === 'pdf'
}
