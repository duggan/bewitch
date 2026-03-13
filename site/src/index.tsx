import { Hono } from 'hono'
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

const app = new Hono()

app.get('/', c => c.html(<Home />))
app.get('/docs', c => c.html(<DocsIndex />))
app.get('/docs/installation', c => c.html(<InstallationDocs />))
app.get('/docs/configuration', c => c.html(<ConfigurationDocs />))
app.get('/docs/collectors', c => c.html(<CollectorsDocs />))
app.get('/docs/tui', c => c.html(<TuiDocs />))
app.get('/docs/alerts', c => c.html(<AlertsDocs />))
app.get('/docs/repl', c => c.html(<ReplDocs />))
app.get('/docs/remote-access', c => c.html(<RemoteAccessDocs />))
app.get('/docs/api', c => c.html(<ApiDocs />))
app.get('/docs/archival', c => c.html(<ArchivalDocs />))

export default app
