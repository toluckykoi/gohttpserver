/** 文件预览弹窗用的轻量正则语法高亮：按 tokenOrder 优先级（注释/字符串优先）
 *  提取 token，输出带 `tk-*` 类名的 HTML。 */

import type { PreviewLanguage } from './previewable'

type TokenType =
  | 'comment'
  | 'string'
  | 'keyword'
  | 'builtin'
  | 'number'
  | 'tag'
  | 'attr'
  | 'property'
  | 'section'
  | 'punct'
  | 'decorator'

interface LanguageDef {
  tokenOrder: TokenType[]
  patterns: Partial<Record<TokenType, RegExp>>
}

const LANGUAGE_DEFS: Record<PreviewLanguage, LanguageDef> = {
  markdown: { tokenOrder: [], patterns: {} },

  json: {
    tokenOrder: ['string', 'builtin', 'number', 'punct'],
    patterns: {
      string: /"(?:[^"\\]|\\.)*"(?=\s*:)|"(?:[^"\\]|\\.)*"/g,
      builtin: /\b(?:true|false|null)\b/g,
      number: /-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b/g,
      punct: /[{}[\],:]/g
    }
  },

  yaml: {
    tokenOrder: ['comment', 'string', 'builtin', 'number', 'section', 'property'],
    patterns: {
      comment: /#[^\n]*/g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g,
      builtin: /\b(?:true|false|null|yes|no|on|off|~)\b/g,
      number: /-?\b\d+(?:\.\d+)?\b/g,
      section: /^[ \t]*[A-Za-z0-9._-]+(?=:)/gm,
      property: /^[ \t]*-?\s*[A-Za-z0-9._-]+(?=:)/gm
    }
  },

  xml: {
    tokenOrder: ['comment', 'tag', 'string', 'attr'],
    patterns: {
      comment: /<!--[\s\S]*?-->/g,
      tag: /<\/?([A-Za-z_][\w:-]*)/g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g,
      attr: /\b([A-Za-z_][\w:-]*)(?==)/g
    }
  },

  toml: {
    tokenOrder: ['comment', 'string', 'builtin', 'number', 'section', 'property'],
    patterns: {
      comment: /#[^\n]*/g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g,
      builtin: /\b(?:true|false)\b/g,
      number: /-?\b\d+(?:\.\d+)?\b/g,
      section: /^\s*\[\[?[A-Za-z0-9._-]+\]?\]\s*$/gm,
      property: /^[ \t]*[A-Za-z0-9._-]+(?==)/gm
    }
  },

  ini: {
    tokenOrder: ['comment', 'string', 'builtin', 'number', 'section', 'property'],
    patterns: {
      comment: /;[^\n]*|\/\/[^\n]*|#[^\n]*/g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g,
      builtin: /\b(?:true|false|yes|no|on|off|null)\b/g,
      number: /-?\b\d+(?:\.\d+)?\b/g,
      section: /^\s*\[[A-Za-z0-9._-]+\]\s*$/gm,
      property: /^[ \t]*[A-Za-z0-9._-]+(?==)/gm
    }
  },

  shell: {
    tokenOrder: ['comment', 'string', 'builtin', 'number', 'keyword'],
    patterns: {
      comment: /#[^\n]*/g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|\$'(?:[^'\\]|\\.)*'/g,
      builtin: /\b(?:true|false)\b/g,
      number: /\b\d+\b/g,
      // 只匹配常见 shell 内建命令，不尝试匹配外部命令。
      keyword:
        /\b(?:if|then|else|elif|fi|for|while|until|do|done|case|esac|in|function|return|exit|export|local|readonly|declare|set|unset|alias|source|eval|exec|shift|break|continue)\b/g
    }
  },

  sql: {
    tokenOrder: ['comment', 'string', 'builtin', 'number', 'keyword'],
    patterns: {
      comment: /--[^\n]*|\/\*[\s\S]*?\*\//g,
      string: /'(?:[^'\\]|\\.)*'/g,
      builtin: /\b(?:true|false|null)\b/g,
      number: /\b\d+(?:\.\d+)?\b/g,
      keyword:
        /\b(?:SELECT|FROM|WHERE|INSERT|INTO|VALUES|UPDATE|SET|DELETE|CREATE|TABLE|INDEX|VIEW|DROP|ALTER|ADD|PRIMARY|KEY|FOREIGN|REFERENCES|JOIN|INNER|OUTER|LEFT|RIGHT|FULL|ON|AS|GROUP|ORDER|BY|HAVING|LIMIT|OFFSET|UNION|ALL|DISTINCT|AND|OR|NOT|NULL|IS|LIKE|IN|BETWEEN|EXISTS|COUNT|SUM|AVG|MIN|MAX|COALESCE|CASE|WHEN|THEN|ELSE|END|WITH|RETURNING|TRUNCATE)\b/gi
    }
  },

  javascript: {
    tokenOrder: ['comment', 'string', 'number', 'decorator', 'keyword', 'builtin'],
    patterns: {
      comment: /\/\/[^\n]*|\/\*[\s\S]*?\*\//g,
      string:
        /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g,
      number: /\b(?:0x[0-9a-fA-F]+|0b[01]+|0o[0-7]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?n?)\b/g,
      decorator: /@[A-Za-z_$][\w$]*/g,
      keyword:
        /\b(?:var|let|const|function|return|if|else|for|while|do|switch|case|break|continue|default|class|extends|new|this|super|import|export|from|as|async|await|try|catch|finally|throw|typeof|instanceof|in|of|void|delete|yield|static|get|set)\b/g,
      builtin: /\b(?:true|false|null|undefined|NaN|Infinity)\b/g
    }
  },

  typescript: {
    tokenOrder: ['comment', 'string', 'number', 'decorator', 'keyword', 'builtin'],
    patterns: {
      comment: /\/\/[^\n]*|\/\*[\s\S]*?\*\//g,
      string:
        /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g,
      number: /\b(?:0x[0-9a-fA-F]+|0b[01]+|0o[0-7]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?n?)\b/g,
      decorator: /@[A-Za-z_$][\w$]*/g,
      keyword:
        /\b(?:var|let|const|function|return|if|else|for|while|do|switch|case|break|continue|default|class|extends|new|this|super|import|export|from|as|async|await|try|catch|finally|throw|typeof|instanceof|in|of|void|delete|yield|static|get|set|public|private|protected|readonly|abstract|implements|interface|type|enum|namespace|declare|module|as|satisfies|keyof|infer|never|unknown|any|void|object|boolean|number|string|symbol|bigint)\b/g,
      builtin: /\b(?:true|false|null|undefined|NaN|Infinity)\b/g
    }
  },

  python: {
    tokenOrder: ['comment', 'string', 'number', 'decorator', 'keyword', 'builtin'],
    patterns: {
      comment: /#[^\n]*/g,
      // 靠 tokenOrder 让三引号先于单引号匹配，避免单引号正则吃掉 docstring 首行。
      string:
        /"""[\s\S]*?"""|'''[\s\S]*?'''|"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g,
      number: /\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?(?:j|j)?\b/g,
      decorator: /@[A-Za-z_][\w]*/g,
      keyword:
        /\b(?:and|as|assert|async|await|break|class|continue|def|del|elif|else|except|finally|for|from|global|if|import|in|is|lambda|nonlocal|not|or|pass|raise|return|try|while|with|yield|True|False|None)\b/g,
      builtin: /\b(?:True|False|None|self|cls)\b/g
    }
  },

  go: {
    tokenOrder: ['comment', 'string', 'number', 'keyword', 'builtin'],
    patterns: {
      comment: /\/\/[^\n]*|\/\*[\s\S]*?\*\//g,
      string: /"(?:[^"\\]|\\.)*"|`[^`]*`/g,
      number: /\b(?:0x[0-9a-fA-F]+|0b[01]+|0o[0-7]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)\b/g,
      keyword:
        /\b(?:break|case|chan|const|continue|default|defer|else|fallthrough|for|func|go|goto|if|import|interface|map|package|range|return|select|struct|switch|type|var)\b/g,
      builtin: /\b(?:true|false|nil|iota|append|cap|close|complex|copy|delete|imag|len|make|new|panic|print|println|real|recover)\b/g
    }
  },

  rust: {
    tokenOrder: ['comment', 'string', 'number', 'decorator', 'keyword', 'builtin'],
    patterns: {
      comment: /\/\/[^\n]*|\/\*[\s\S]*?\*\//g,
      string: /b?"(?:[^"\\]|\\.)*"|b?r#*"[\s\S]*?"#*/g,
      number: /\b(?:0x[0-9a-fA-F_]+|0o[0-7_]+|0b[01_]+|\d[\d_]*(?:\.\d[\d_]*)?(?:[eE][+-]?\d[\d_]*)?(?:f32|f64|i\d+|u\d+|usize|isize)?)\b/g,
      decorator: /#!?\[[^\]]*\]|#!?[A-Za-z_][\w]*/g,
      keyword:
        /\b(?:as|async|await|break|const|continue|crate|dyn|else|enum|extern|false|fn|for|if|impl|in|let|loop|match|mod|move|mut|pub|ref|return|Self|self|static|struct|super|trait|true|type|unsafe|use|where|while|box|do|final|macro|override|priv|try|typeof|unsized|virtual|yield)\b/g,
      builtin: /\b(?:true|false|None|Some|Ok|Err)\b/g
    }
  },

  java: {
    tokenOrder: ['comment', 'string', 'number', 'decorator', 'keyword', 'builtin'],
    patterns: {
      comment: /\/\/[^\n]*|\/\*[\s\S]*?\*\//g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g,
      number: /\b(?:0x[0-9a-fA-F]+|0b[01]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?[fFdDlL]?)\b/g,
      decorator: /@[A-Za-z_][\w.]*/g,
      keyword:
        /\b(?:abstract|assert|boolean|break|byte|case|catch|char|class|const|continue|default|do|double|else|enum|extends|final|finally|float|for|goto|if|implements|import|instanceof|int|interface|long|native|new|package|private|protected|public|return|short|static|strictfp|super|switch|synchronized|this|throw|throws|transient|try|void|volatile|while|yield|var|record|sealed|permits)\b/g,
      builtin: /\b(?:true|false|null)\b/g
    }
  },

  cpp: {
    tokenOrder: ['comment', 'string', 'number', 'keyword', 'builtin'],
    patterns: {
      comment: /\/\/[^\n]*|\/\*[\s\S]*?\*\//g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|R"\([^)]*\)\([^"]*\)\"/g,
      number: /\b(?:0x[0-9a-fA-F]+|\d+(?:\.\d+)?(?:[eE][+-]?\d+)?[fFlLuU]*)\b/g,
      keyword:
        /\b(?:auto|break|case|catch|class|const|constexpr|continue|decltype|default|delete|do|else|enum|explicit|export|extern|false|final|for|friend|goto|if|inline|mutable|namespace|new|noexcept|nullptr|operator|override|private|protected|public|register|reinterpret_cast|return|sizeof|static|static_cast|struct|switch|template|this|throw|true|try|typedef|typeid|typename|union|using|virtual|void|volatile|while)\b/g,
      builtin: /\b(?:true|false|null|nullptr|TRUE|FALSE|NULL)\b/g
    }
  },

  css: {
    tokenOrder: ['comment', 'string', 'number', 'keyword', 'section', 'property'],
    patterns: {
      comment: /\/\*[\s\S]*?\*\//g,
      string: /"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/g,
      number: /-?\b\d+(?:\.\d+)?(?:px|em|rem|%|vh|vw|pt|pc|in|cm|mm|ex|ch|fr|s|ms|deg|rad|turn)?\b/g,
      keyword:
        /@?(?:media|import|charset|namespace|supports|font-face|keyframes|important|root|page|document)\b/g,
      section: /[.#][A-Za-z_][\w-]*/g,
      property: /\b[a-z-]+(?=\s*:)/g
    }
  },

  plain: { tokenOrder: [], patterns: {} }
}

const ESCAPE_MAP: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;'
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ESCAPE_MAP[c])
}

/** 按语言高亮 `code`，返回安全 HTML。 */
export function highlight(code: string, language: PreviewLanguage): string {
  const def = LANGUAGE_DEFS[language]
  if (!def || def.tokenOrder.length === 0) {
    return escapeHtml(code)
  }

  // 把 token 暂存到不会与源码或后续正则冲突的占位符里。
  const stash: string[] = []
  const tag = (text: string, type: TokenType) => {
    const idx = stash.length
    stash.push(`<span class="tk-${type}">${escapeHtml(text)}</span>`)
    return `TK${idx}`
  }

  let work = code
  for (const tokenType of def.tokenOrder) {
    const pattern = def.patterns[tokenType]
    if (!pattern) continue
    const regex = pattern.flags.includes('g')
      ? pattern
      : new RegExp(pattern.source, pattern.flags + 'g')
    work = work.replace(regex, (m) => tag(m, tokenType))
  }

  // 未命中 token 的空隙做转义。
  const parts = work.split(/(TK\d+)/)
  for (let i = 0; i < parts.length; i++) {
    if (!/^TK\d+$/.test(parts[i])) {
      parts[i] = escapeHtml(parts[i])
    }
  }

  // 还原暂存的 token。
  return parts.join('').replace(/TK(\d+)/g, (_, n) => stash[Number(n)])
}

/** 按行高亮，避免跨行 token 的颜色串到其它行。 */
export function highlightLines(code: string, language: PreviewLanguage): string[] {
  if (language === 'plain' || !LANGUAGE_DEFS[language] || LANGUAGE_DEFS[language].tokenOrder.length === 0) {
    return code.split('\n').map(escapeHtml)
  }
  return code.split('\n').map((line) => highlight(line, language))
}
