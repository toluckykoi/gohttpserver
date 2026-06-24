import {
  FolderOpened,
  Document,
  DocumentCopy,
  Picture,
  Headset,
  VideoCamera,
  Apple,
  Box,
  Monitor,
  Reading,
  Folder
} from '@element-plus/icons-vue'
import type { Component } from 'vue'

const iconMap: Record<string, Component> = {
  folder: FolderOpened,
  default: Document,
  code: DocumentCopy,
  pdf: Reading,
  zip: Box,
  audio: Headset,
  image: Picture,
  video: VideoCamera,
  apple: Apple,
  windows: Monitor
}

export function getFileIcon(filename: string, type: 'file' | 'dir'): Component {
  if (type === 'dir') {
    if (filename === '.git') {
      return Folder
    }
    return iconMap.folder
  }

  const ext = filename.split('.').pop()?.toLowerCase() || ''

  switch (ext) {
    case 'go':
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
      return iconMap.code
    case 'pdf':
      return iconMap.pdf
    case 'zip':
    case 'rar':
    case '7z':
    case 'tar':
    case 'gz':
      return iconMap.zip
    case 'mp3':
    case 'wav':
    case 'flac':
    case 'aac':
    case 'ogg':
    case 'm4a':
      return iconMap.audio
    case 'jpg':
    case 'jpeg':
    case 'png':
    case 'gif':
    case 'bmp':
    case 'svg':
    case 'webp':
    case 'tiff':
      return iconMap.image
    case 'mp4':
    case 'webm':
    case 'avi':
    case 'mkv':
    case 'mov':
    case 'wmv':
      return iconMap.video
    case 'ipa':
    case 'dmg':
      return iconMap.apple
    case 'apk':
      return Box
    case 'exe':
    case 'msi':
      return iconMap.windows
    default:
      return iconMap.default
  }
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

export function isAudioFile(filename: string): boolean {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  return [
    'mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a', 'opus', 'wma'
  ].includes(ext)
}

export function isPdfFile(filename: string): boolean {
  return filename.split('.').pop()?.toLowerCase() === 'pdf'
}