import { Hono } from 'hono'
import { docsBase } from './docs-base'
import { Home } from './pages/home'
import { DocsIndex } from './pages/docs/index'
import { InstallationDocs } from './pages/docs/installation'
import { ConfigurationDocs } from './pages/docs/configuration'
import { CollectorsDocs } from './pages/docs/collectors'
import { TuiDocs } from './pages/docs/tui'
import { AlertsDocs } from './pages/docs/alerts'
import { ReplDocs } from './pages/docs/repl'
import { RemoteAccessDocs } from './pages/docs/remote-access'
import { ApiDocs } from './pages/docs/api'
import { ArchivalDocs } from './pages/docs/archival'
import { ChangelogDocs } from './pages/docs/changelog'

const app = new Hono()

app.get('/', c => c.html(<Home />))
app.get(docsBase, c => c.html(<DocsIndex />))
app.get(`${docsBase}/installation`, c => c.html(<InstallationDocs />))
app.get(`${docsBase}/configuration`, c => c.html(<ConfigurationDocs />))
app.get(`${docsBase}/collectors`, c => c.html(<CollectorsDocs />))
app.get(`${docsBase}/tui`, c => c.html(<TuiDocs />))
app.get(`${docsBase}/alerts`, c => c.html(<AlertsDocs />))
app.get(`${docsBase}/repl`, c => c.html(<ReplDocs />))
app.get(`${docsBase}/remote-access`, c => c.html(<RemoteAccessDocs />))
app.get(`${docsBase}/api`, c => c.html(<ApiDocs />))
app.get(`${docsBase}/archival`, c => c.html(<ArchivalDocs />))
app.get(`${docsBase}/changelog`, c => c.html(<ChangelogDocs />))

export default app
