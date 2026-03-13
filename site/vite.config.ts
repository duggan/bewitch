import devServer from '@hono/vite-dev-server'
import ssg from '@hono/vite-ssg'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig, type Plugin } from 'vite'
import { readdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

// After build, rewrite CSS links in generated HTML to point to the hashed CSS file
function rewriteCssLinks(): Plugin {
  return {
    name: 'rewrite-css-links',
    apply: 'build',
    async writeBundle(options) {
      const outDir = options.dir || 'dist'
      const assets = await readdir(join(outDir, 'assets')).catch(() => [] as string[])
      const cssFile = (assets as string[]).find(f => f.endsWith('.css'))
      if (!cssFile) return

      const htmlFiles: string[] = []
      async function walk(dir: string) {
        const entries = await readdir(dir, { withFileTypes: true })
        for (const entry of entries) {
          const path = join(dir, entry.name)
          if (entry.isDirectory()) await walk(path)
          else if (entry.name.endsWith('.html')) htmlFiles.push(path)
        }
      }
      await walk(outDir)

      for (const file of htmlFiles) {
        let html = await readFile(file, 'utf-8')
        html = html.replace(
          '<link rel="stylesheet" href="/src/global.css"/>',
          `<link rel="stylesheet" href="/assets/${cssFile}"/>`
        )
        await writeFile(file, html)
      }
    },
  }
}

export default defineConfig({
  plugins: [
    tailwindcss(),
    ssg({ entry: 'src/index.tsx' }),
    devServer({ entry: 'src/index.tsx' }),
    rewriteCssLinks(),
  ],
  build: {
    rollupOptions: {
      input: ['src/client.ts'],
      output: {
        entryFileNames: 'assets/client.js',
        assetFileNames: 'assets/[name].[hash][extname]',
      },
    },
  },
})
