import devServer from '@hono/vite-dev-server'
import ssg from '@hono/vite-ssg'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig, type Plugin } from 'vite'
import { readdir, readFile, writeFile } from 'node:fs/promises'
import { join, resolve } from 'node:path'

const version = (await readFile(resolve(__dirname, '../VERSION'), 'utf-8')).trim()
const latestStable = (await readFile(resolve(__dirname, '../LATEST_STABLE'), 'utf-8')).trim()

// After build, rewrite CSS/JS links in generated HTML to point to hashed asset files
function rewriteAssetLinks(): Plugin {
  return {
    name: 'rewrite-asset-links',
    apply: 'build',
    async writeBundle(options) {
      const outDir = options.dir || 'dist'
      const assets = await readdir(join(outDir, 'assets')).catch(() => [] as string[])
      const cssFile = (assets as string[]).find(f => f.endsWith('.css'))

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
        if (cssFile) {
          html = html.replace(
            '<link rel="stylesheet" href="/src/global.css"/>',
            `<link rel="stylesheet" href="/assets/${cssFile}"/>`
          )
        }
        html = html.replace(
          '<script type="module" src="/src/client.ts"></script>',
          '<script type="module" src="/assets/client.js"></script>'
        )
        await writeFile(file, html)
      }
    },
  }
}

// After build, stamp the VERSION into install.sh (which is copied from public/)
function stampInstallVersion(): Plugin {
  return {
    name: 'stamp-install-version',
    apply: 'build',
    async writeBundle(options) {
      const outDir = options.dir || 'dist'
      const installSh = join(outDir, 'install.sh')
      try {
        let content = await readFile(installSh, 'utf-8')
        content = content.replace(/^VERSION=".*"$/m, `VERSION="${version}"`)
        await writeFile(installSh, content)
      } catch {}
    },
  }
}

export default defineConfig({
  plugins: [
    tailwindcss(),
    ssg({ entry: 'src/index.tsx' }),
    devServer({ entry: 'src/index.tsx' }),
    rewriteAssetLinks(),
    stampInstallVersion(),
  ],
  define: {
    __BEWITCH_VERSION__: JSON.stringify(version),
    __BEWITCH_LATEST_STABLE__: JSON.stringify(latestStable),
  },
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
