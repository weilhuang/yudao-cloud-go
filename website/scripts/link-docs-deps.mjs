import { lstat, readlink, stat, symlink } from 'node:fs/promises'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const websiteDir = fileURLToPath(new URL('..', import.meta.url))
const modulesDir = resolve(websiteDir, 'node_modules')
const link = resolve(websiteDir, '../docs/node_modules')

// VitePress 以 docs/ 为 Vite 根目录，需要从这里解析 website/ 的依赖。
if (!(await stat(modulesDir).catch(() => null))?.isDirectory()) {
  throw new Error('请先在 website/ 运行 npm ci')
}

const existing = await lstat(link).catch((error) => {
  if (error.code === 'ENOENT') return null
  throw error
})
if (existing) {
  const target = existing.isSymbolicLink() ? resolve(dirname(link), await readlink(link)) : null
  if (target !== modulesDir) {
    throw new Error('docs/node_modules 已被其他内容占用，请先手动检查')
  }
} else {
  await symlink(relative(dirname(link), modulesDir), link, 'dir')
}
