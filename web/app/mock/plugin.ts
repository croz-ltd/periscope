import type { Plugin } from 'vite'
import { mockCSV, mockCalendar, mockChanges, mockMatrix, mockTimeline, mockUser } from './fleet'
import pkg from '../package.json'

// Serves the mock fleet over the same REST endpoints the Go server exposes, as
// dev-server middleware rather than anything the app knows about. The app makes
// its normal fetch calls and cannot tell the difference, no mock branch exists in
// src/, and nothing here can reach a production bundle: this plugin is only
// registered for `vite --mode mock` (npm run dev:mock).
export function mockApi(): Plugin {
  return {
    name: 'periscope-mock-api',
    configureServer(server) {
      // Mounted at /api, so req.url arrives with that prefix stripped.
      server.middlewares.use('/api', (req, res) => {
        const url = new URL(req.url ?? '/', 'http://mock')
        const q = url.searchParams

        const json = (body: unknown, status = 200) => {
          res.statusCode = status
          res.setHeader('Content-Type', 'application/json')
          res.end(JSON.stringify(body))
        }

        switch (url.pathname) {
          case '/matrix':
            return json(mockMatrix(q.get('at') ?? undefined))
          case '/changes':
            return json(
              mockChanges({
                from: q.get('from') ?? undefined,
                to: q.get('to') ?? undefined,
                cluster: q.get('cluster') ?? undefined,
                limit: q.get('limit') ? Number(q.get('limit')) : undefined,
                counters: q.get('counters') !== 'false',
              }),
            )
          case '/changes/calendar':
            return json(mockCalendar())
          case '/timeline': {
            const days = Number(q.get('days') ?? 7)
            const keys = (q.get('key') ?? '').split(',').filter(Boolean)
            if (keys.length === 0) return json({ error: 'at least one key is required' }, 400)
            return json(mockTimeline(keys, days, q.get('at') ?? undefined))
          }
          case '/export.json':
            return json(mockMatrix(q.get('at') ?? undefined))
          case '/export.csv':
            res.setHeader('Content-Type', 'text/csv')
            res.setHeader('Content-Disposition', 'attachment; filename="periscope-mock.csv"')
            return res.end(mockCSV())
          case '/refresh':
            // A scrape has nothing to do here, but answering the way the server
            // does keeps the Refresh action honest instead of erroring.
            res.statusCode = 202
            return res.end()
          case '/user':
            return json(mockUser)
          case '/version':
            return json({ version: `${pkg.version}+mock` })
          default:
            return json({ error: `no mock for ${url.pathname}` }, 404)
        }
      })
    },
  }
}
