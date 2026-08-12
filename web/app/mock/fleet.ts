// A synthetic fleet, so the UI can be developed, reviewed and screenshotted
// without eight clusters to point it at. This is the only place mock data lives,
// it is typed against the real API interfaces so it cannot drift from the shapes
// the server sends, and it is never bundled: only the mock Vite plugin (see
// mock/plugin.ts, enabled with `npm run dev:mock`) imports it.
//
// The numbers are made up but the shapes are not. Row keys, comparison kinds and
// group titles are the ones the extractors in internal/extract actually emit, so
// what you see here is what a real fleet renders as.
import type {
  Cell,
  Change,
  ChangeDay,
  ClusterInfo,
  CompareKind,
  Matrix,
  MatrixGroup,
  Page,
  Row,
} from '../src/api'

// Deterministic PRNG (mulberry32). A fixture that reshuffled every run would
// make every screenshot a fresh diff and every UI review a moving target.
function rng(seed: number): () => number {
  let a = seed
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

const SEED = 20260811
let rand = rng(SEED)
const between = (lo: number, hi: number) => lo + Math.floor(rand() * (hi - lo + 1))

// Every response starts from the same seed. Without this the generator carries
// on where the last payload left it, so a page reload reshuffles half the
// operator versions: the fixture looks like a fleet churning, and a screenshot
// cannot be retaken.
const reseed = () => {
  rand = rng(SEED)
}

const MINUTE = 60_000
const DAY = 24 * 60 * MINUTE

interface MockCluster {
  name: string
  label?: string
  color?: string
  bgColor?: string
  staleMinutes?: number
  error?: string
}

const PROD = { color: '#ffffff', bgColor: '#0066cc' }
const STAGE = { color: '#ffffff', bgColor: '#8476d1' }
const DEV = { color: '#ffffff', bgColor: '#3d7317' }

// A believable fleet: two production regions, a staging cluster, a dev cluster
// and two edge sites, plus the hub itself joined like any other cluster. One is
// stale and one is answering with an extractor error, because a fleet view that
// only ever shows the happy path teaches nothing about reading it.
const CLUSTERS: MockCluster[] = [
  { name: 'hub-eu-central' },
  { name: 'prod-eu-central', label: 'PROD EU', ...PROD },
  { name: 'prod-eu-west', label: 'PROD EU', ...PROD },
  { name: 'prod-us-east', label: 'PROD US', ...PROD },
  { name: 'stage-eu-central', label: 'STAGE', ...STAGE, staleMinutes: 96 },
  { name: 'dev-eu-central', label: 'DEV', ...DEV },
  { name: 'edge-site-01', error: 'clusterserviceversions.operators.coreos.com is forbidden' },
  { name: 'edge-site-02' },
]

const names = CLUSTERS.map((c) => c.name)
const prod = names.filter((n) => n.startsWith('prod'))
const edge = names.filter((n) => n.startsWith('edge'))

function clusters(now: number): ClusterInfo[] {
  return CLUSTERS.map((c, i) => ({
    name: c.name,
    time: new Date(now - (c.staleMinutes ?? between(1, 6)) * MINUTE).toISOString(),
    ok: !c.error,
    stale: !!c.staleMinutes,
    order: i * 10,
    ...(c.error ? { error: c.error } : {}),
    ...(c.label ? { label: c.label, color: c.color, bgColor: c.bgColor } : {}),
  }))
}

// Version comparison, scored the way internal/drift does: the fleet-max leads
// and the gap score decides how dark a behind cell shades.
function parse(v: string): number[] | null {
  const core = v.replace(/^v/, '').split('+')[0].split('-')[0]
  const parts = core.split('.').map(Number)
  return parts.length && parts.every((n) => Number.isInteger(n) && n >= 0) ? parts : null
}

function gap(v: number[], leader: number[]): { severity: number; gapKind: string } {
  const [vMaj = 0, vMin = 0, vPatch = 0] = v
  const [lMaj = 0, lMin = 0, lPatch = 0] = leader
  if (lMaj !== vMaj) return { severity: (lMaj - vMaj) * 10000 + 10000, gapKind: 'major' }
  if (lMin !== vMin) return { severity: (lMin - vMin) * 100 + 100, gapKind: 'minor' }
  if (lPatch !== vPatch) return { severity: lPatch - vPatch, gapKind: 'patch' }
  return { severity: 1, gapKind: 'prerelease' }
}

type Values = Record<string, string | null>

interface RowSpec {
  key: string
  name: string
  kind: string
  group: string
  compare: CompareKind
  values: Values
  extra?: Record<string, Record<string, string>>
  namespace?: string
}

function cellsOf(spec: RowSpec, now: number): { leader: string; cells: Record<string, Cell> } {
  const cells: Record<string, Cell> = {}
  const base = (cluster: string, version: string): Cell => ({
    cluster,
    version,
    state: 'info',
    severity: 0,
    ...(spec.namespace ? { namespace: spec.namespace } : {}),
    ...(spec.extra?.[cluster] ? { extra: spec.extra[cluster] } : {}),
  })
  let leader = ''

  if (spec.compare === 'version') {
    const parsed = Object.values(spec.values)
      .filter((v): v is string => !!v)
      .map(parse)
      .filter((v): v is number[] => !!v)
    const top = parsed.sort((a, b) => b[0] - a[0] || b[1] - a[1] || b[2] - a[2])[0]
    leader =
      Object.entries(spec.values).find(([, v]) => v && String(parse(v)) === String(top))?.[1] ?? ''
    for (const [cluster, v] of Object.entries(spec.values)) {
      if (v === null) continue
      const p = parse(v)
      const cell = base(cluster, v)
      if (!p) cell.state = 'unknown'
      else if (String(p) === String(top)) cell.state = 'leader'
      else Object.assign(cell, { state: 'behind', ...gap(p, top) })
      cells[cluster] = cell
    }
  } else if (spec.compare === 'match') {
    const counts = new Map<string, number>()
    for (const v of Object.values(spec.values)) {
      if (v !== null) counts.set(v, (counts.get(v) ?? 0) + 1)
    }
    leader = [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0][0]
    for (const [cluster, v] of Object.entries(spec.values)) {
      if (v === null) continue
      cells[cluster] = { ...base(cluster, v), state: v === leader ? 'match' : 'mismatch' }
    }
  } else if (spec.compare === 'expiry') {
    // Expiry rows carry days-to-expiry here and are turned into the date the API
    // sends, so a fixture kept for months never renders as long expired.
    for (const [cluster, v] of Object.entries(spec.values)) {
      if (v === null) continue
      const days = Number(v)
      const state = days <= 60 ? 'expiry_crit' : days <= 120 ? 'expiry_warn' : 'expiry_ok'
      cells[cluster] = {
        ...base(cluster, new Date(now + days * DAY).toISOString().slice(0, 10)),
        state,
        severity: days,
      }
    }
  } else {
    for (const [cluster, v] of Object.entries(spec.values)) {
      if (v !== null) cells[cluster] = base(cluster, v)
    }
  }

  for (const n of names) {
    if (!cells[n]) cells[n] = { cluster: n, state: 'not_installed', severity: 0 }
  }
  return { leader, cells }
}

// spread assigns a value per cluster, so a row can say "these clusters are
// behind" without spelling out all eight.
function spread(fallback: string | null, overrides: Values = {}): Values {
  const out: Values = {}
  for (const n of names) out[n] = n in overrides ? overrides[n] : fallback
  return out
}

const OPERATORS: [key: string, name: string, leader: string, behind: string][] = [
  ['grafana-operator', 'Grafana Operator', '5.18.0', '5.15.1'],
  ['advanced-cluster-management', 'Advanced Cluster Management', '2.12.2', '2.11.4'],
  ['cluster-logging', 'Red Hat OpenShift Logging', '6.1.1', '5.9.9'],
  ['compliance-operator', 'Compliance Operator', '1.6.1', '1.5.1'],
  ['kiali-ossm', 'Kiali Operator', '1.89.7', '1.73.0'],
  ['local-storage-operator', 'Local Storage', '4.17.0', '4.16.0'],
  ['openshift-cert-manager-operator', 'cert-manager Operator', '1.15.1', '1.14.5'],
  ['openshift-gitops-operator', 'Red Hat OpenShift GitOps', '1.14.3', '1.12.6'],
  ['openshift-pipelines-operator-rh', 'Red Hat OpenShift Pipelines', '1.17.1', '1.14.5'],
  ['rhsso-operator', 'Red Hat Single Sign-On', '7.6.11', '7.6.8'],
  ['web-terminal', 'Web Terminal', '1.11.0', '1.10.1'],
]

// Values the change feed refers to. A feed entry must not end on a version the
// matrix does not show, or the fixture argues with itself.
const OPERATOR_PINS: Record<string, Values> = {
  // The README's screenshot rests on this pair: the newest operator running an
  // older Grafana than the oldest operator does, which is the whole argument for
  // extracting the managed version separately.
  'grafana-operator': { 'prod-eu-west': '5.18.0', 'dev-eu-central': '5.18.0', 'prod-us-east': '5.15.1' },
  'compliance-operator': { 'edge-site-02': '1.5.1' },
  'web-terminal': { 'prod-us-east': null },
  'openshift-pipelines-operator-rh': { 'prod-eu-central': '1.17.1' },
  'openshift-cert-manager-operator': { 'dev-eu-central': '1.14.5' },
}

function rowSpecs(): RowSpec[] {
  const specs: RowSpec[] = [
    {
      key: 'openshift',
      name: 'OpenShift',
      kind: 'openshift',
      group: 'OpenShift',
      compare: 'version',
      values: spread('4.17.9', {
        'stage-eu-central': '4.16.21',
        'edge-site-01': '4.14.33',
        'edge-site-02': '4.16.21',
      }),
    },
    {
      key: 'openshift-channel',
      name: 'Update channel',
      kind: 'openshift',
      group: 'OpenShift',
      compare: 'match',
      values: spread('stable-4.17', {
        'dev-eu-central': 'fast-4.17',
        'edge-site-01': 'stable-4.14',
      }),
    },
    {
      key: 'openshift-upgradeable',
      name: 'Upgrade blocked',
      kind: 'openshift',
      group: 'OpenShift',
      compare: 'match',
      values: spread('no', { 'edge-site-01': 'AdminAckRequired' }),
    },
    {
      key: 'default-storageclass',
      name: 'StorageClass - Default',
      kind: 'storage',
      group: 'OpenShift',
      compare: 'match',
      values: spread('ocs-storagecluster-ceph-rbd', {
        'edge-site-01': 'local-path',
        'edge-site-02': 'local-path',
      }),
    },
    {
      key: 'node-kubelet',
      name: 'Kubelet',
      kind: 'nodes',
      group: 'Node',
      compare: 'version',
      values: spread('1.30.6', {
        'stage-eu-central': '1.29.8',
        'edge-site-01': '1.27.16',
        'edge-site-02': '1.29.8',
      }),
    },
    {
      key: 'mcp-master',
      name: 'MCP: master',
      kind: 'mcp',
      group: 'MachineConfigPools',
      compare: 'match',
      values: spread('Updated'),
    },
    {
      key: 'mcp-worker',
      name: 'MCP: worker',
      kind: 'mcp',
      group: 'MachineConfigPools',
      compare: 'match',
      values: spread('Updated', { 'stage-eu-central': 'Updating (3/9)' }),
    },
    {
      key: 'cert-api',
      name: 'API',
      kind: 'cert',
      group: 'Certificate',
      compare: 'expiry',
      values: spread('318', {
        'prod-eu-west': '104',
        'stage-eu-central': '47',
        'edge-site-02': '211',
      }),
    },
    {
      key: 'cert-ingress',
      name: 'Ingress',
      kind: 'cert',
      group: 'Certificate',
      compare: 'expiry',
      values: spread('276', {
        'prod-us-east': '88',
        'dev-eu-central': '19',
        'edge-site-01': '133',
      }),
    },
    {
      key: 'virtualization',
      name: 'OpenShift Virtualization',
      kind: 'virt',
      group: 'OpenShift Virtualization',
      compare: 'version',
      values: spread(null, {
        'prod-eu-central': '4.17.4',
        'prod-eu-west': '4.17.4',
        'prod-us-east': '4.16.7',
        'stage-eu-central': '4.16.7',
      }),
    },
    {
      key: 'hco-cpu-ratio',
      name: 'vCPU allocation ratio',
      kind: 'virt',
      group: 'OpenShift Virtualization',
      compare: 'match',
      values: spread(null, {
        'prod-eu-central': '10',
        'prod-eu-west': '10',
        'prod-us-east': '10',
        'stage-eu-central': '4',
      }),
    },
    // The row this whole app exists for the sake of arguments about: the managed
    // product version, which no operator row reports.
    {
      key: 'grafana',
      name: 'Grafana',
      kind: 'managed',
      group: 'Operators',
      compare: 'version',
      namespace: 'grafana',
      values: spread('12.0.2', {
        'prod-us-east': '11.6.1',
        'stage-eu-central': '11.6.1',
        'dev-eu-central': '10.4.14',
        'edge-site-01': null,
        'edge-site-02': null,
      }),
      extra: {
        'prod-us-east': { 'grafana/fleet-central': '12.0.2', 'team-payments/grafana': '11.6.1' },
      },
    },
    {
      key: 'portworx-csi',
      name: 'Portworx (CSI)',
      kind: 'csi',
      group: 'Operators',
      compare: 'version',
      values: spread(null, {
        'prod-eu-central': '3.2.1',
        'prod-eu-west': '3.2.1',
        'prod-us-east': '3.1.0',
      }),
    },
  ]

  for (const [key, name, leader, behind] of OPERATORS) {
    const values = spread(leader)
    for (const n of names) {
      if (rand() < 0.35) values[n] = behind
    }
    values['edge-site-01'] = null // the cluster whose CSV read is forbidden
    if (rand() < 0.3) values['edge-site-02'] = null
    Object.assign(values, OPERATOR_PINS[key] ?? {})
    specs.push({ key, name, kind: 'operator', group: 'Operators', compare: 'version', values })
  }

  // Counts and inventory: informational, so they land on the Statistics page.
  const storage = volumes()
  specs.push(
    {
      key: 'node-count',
      name: 'Total nodes',
      kind: 'nodes',
      group: 'Node',
      compare: 'info',
      values: spread(null, {
        'hub-eu-central': '6',
        'prod-eu-central': '24',
        'prod-eu-west': '21',
        'prod-us-east': '18',
        'stage-eu-central': '9',
        'dev-eu-central': '6',
        'edge-site-01': '3',
        'edge-site-02': '3',
      }),
    },
    {
      key: 'openshift-update-available',
      name: 'Update available',
      kind: 'openshift',
      group: 'OpenShift',
      compare: 'info',
      values: spread('4.17.12', {
        'stage-eu-central': '4.16.24',
        'dev-eu-central': '-',
        'edge-site-01': '4.14.42',
      }),
    },
    {
      key: 'olm-updates-pending',
      name: 'Operator updates pending',
      kind: 'operator',
      group: 'Operators',
      compare: 'info',
      values: spread('0', {
        'prod-us-east': '3',
        'stage-eu-central': '1',
        'edge-site-02': '5',
      }),
      extra: {
        'prod-us-east': {
          'cluster-logging': 'cluster-logging.v5.9.9 -> cluster-logging.v6.1.1',
          'kiali-ossm': 'kiali-operator.v1.73.0 -> kiali-operator.v1.89.7',
          'rhsso-operator': 'rhsso-operator.v7.6.8 -> rhsso-operator.v7.6.11',
        },
      },
    },
    {
      key: 'olm-installplans-waiting',
      name: 'InstallPlans awaiting approval',
      kind: 'operator',
      group: 'Operators',
      compare: 'info',
      values: spread('0', { 'prod-us-east': '2', 'edge-site-02': '1' }),
    },
    {
      key: 'storage-total',
      name: 'Storage (all classes)',
      kind: 'storage',
      group: 'Storage',
      compare: 'info',
      values: storage.total,
    },
    {
      key: 'storage-ocs-storagecluster-ceph-rbd',
      name: 'Storage: ocs-storagecluster-ceph-rbd',
      kind: 'storage',
      group: 'Storage',
      compare: 'info',
      values: storage.ceph,
    },
    {
      key: 'storage-local-path',
      name: 'Storage: local-path',
      kind: 'storage',
      group: 'Storage',
      compare: 'info',
      values: storage.localPath,
    },
    {
      key: 'vm-total',
      name: 'Virtual machines',
      kind: 'virt',
      group: 'OpenShift Virtualization',
      compare: 'info',
      values: spread(null, {
        'prod-eu-central': '37',
        'prod-eu-west': '31',
        'prod-us-east': '24',
        'stage-eu-central': '11',
      }),
    },
    {
      key: 'vm-running',
      name: 'VMs running',
      kind: 'virt',
      group: 'OpenShift Virtualization',
      compare: 'info',
      values: spread(null, {
        'prod-eu-central': '35',
        'prod-eu-west': '30',
        'prod-us-east': '21',
        'stage-eu-central': '7',
      }),
    },
    {
      key: 'vm-stopped',
      name: 'VMs stopped',
      kind: 'virt',
      group: 'OpenShift Virtualization',
      compare: 'info',
      values: spread(null, {
        'prod-eu-central': '2',
        'prod-eu-west': '1',
        'prod-us-east': '3',
        'stage-eu-central': '4',
      }),
    },
    {
      key: 'vm-templates',
      name: 'VM templates',
      kind: 'virt',
      group: 'OpenShift Virtualization',
      compare: 'info',
      values: spread(null, {
        'prod-eu-central': '18',
        'prod-eu-west': '18',
        'prod-us-east': '14',
        'stage-eu-central': '18',
      }),
    },
  )
  return specs
}

// Volume counts per storage class, with the "all classes" row summed from them:
// a total under one of its own classes reads as a bug in the app, not the data.
function volumes(): { total: Values; ceph: Values; localPath: Values } {
  const counts = new Map<string, [pvc: number, pv: number][]>(names.map((n) => [n, []]))
  const perClass = (clusters: string[], lo: number, hi: number): Values => {
    const out: Values = spread(null)
    for (const n of clusters) {
      // Every bound claim has a volume, plus a few volumes nothing claims, so PV
      // never falls below PVC, which two independent random numbers allow.
      const pvc = between(lo, hi)
      const pair: [number, number] = [pvc, pvc + between(0, 9)]
      counts.get(n)!.push(pair)
      out[n] = `${pair[0]} PVC / ${pair[1]} PV`
    }
    return out
  }
  const ceph = perClass([...prod, 'hub-eu-central', 'stage-eu-central', 'dev-eu-central'], 12, 160)
  const localPath = perClass(edge, 2, 20)
  const total: Values = spread(null)
  for (const n of names) {
    const own = counts.get(n)!
    if (own.length === 0) continue
    const pvc = own.reduce((sum, [c]) => sum + c, 0)
    const pv = own.reduce((sum, [, v]) => sum + v, 0)
    total[n] = `${pvc} PVC / ${pv} PV`
  }
  return { total, ceph, localPath }
}

// Page assembly mirrors internal/drift: info rows go to Statistics, everything
// else to Compare, sections keep the built-in order, rows sort by name.
const GROUP_ORDER = [
  'OpenShift',
  'Node',
  'MachineConfigPools',
  'Storage',
  'Certificate',
  'OpenShift Virtualization',
  'Operators',
]

function pagesOf(rows: Row[]): Page[] {
  const build = (wantInfo: boolean): MatrixGroup[] => {
    const groups: MatrixGroup[] = []
    for (const title of GROUP_ORDER) {
      const keys = rows
        .filter((r) => r.group === title && (r.compare === 'info') === wantInfo)
        .sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))
        .map((r) => r.key)
      if (keys.length > 0) groups.push({ title, keys })
    }
    return groups
  }
  return [
    { id: 'compare', title: 'Compare', groups: build(false) },
    { id: 'statistics', title: 'Statistics', groups: build(true) },
  ]
}

export function mockMatrix(at?: string): Matrix {
  reseed()
  const now = at ? Date.parse(at) : Date.now()
  const rows: Row[] = rowSpecs().map((spec) => {
    const { leader, cells } = cellsOf(spec, now)
    return {
      key: spec.key,
      name: spec.name,
      group: spec.group,
      compare: spec.compare,
      kind: spec.kind,
      leader,
      cells,
    }
  })
  rows.sort((a, b) => a.key.localeCompare(b.key))
  return {
    clusters: clusters(now),
    rows,
    pages: pagesOf(rows),
    ...(at ? { at } : {}),
  }
}

// A change feed with the same texture as a real one: a few operator updates a
// week, the odd cluster dropping out and coming back, and counters moving
// constantly, which is exactly why the UI hides them by default.
const FEED: [daysAgo: number, hour: number, cluster: string, change: Omit<Change, 'time' | 'cluster'>][] = [
  [0, 9, 'prod-eu-central', { kind: 'updated', key: 'grafana', name: 'Grafana', compare: 'version', from: '11.6.1', to: '12.0.2' }],
  [0, 8, 'stage-eu-central', { kind: 'unreachable', to: 'context deadline exceeded' }],
  [0, 7, 'prod-eu-central', { kind: 'updated', key: 'node-count', name: 'Total nodes', compare: 'info', from: '23', to: '24' }],
  [1, 16, 'dev-eu-central', { kind: 'updated', key: 'openshift', name: 'OpenShift', compare: 'version', from: '4.17.6', to: '4.17.9' }],
  [1, 15, 'dev-eu-central', { kind: 'updated', key: 'node-kubelet', name: 'Kubelet', compare: 'version', from: '1.29.8', to: '1.30.6' }],
  [1, 11, 'dev-eu-central', { kind: 'updated', key: 'openshift-channel', name: 'Update channel', compare: 'match', from: 'stable-4.17', to: 'fast-4.17' }],
  [2, 14, 'prod-eu-west', { kind: 'updated', key: 'grafana-operator', name: 'Grafana Operator', compare: 'version', from: '5.15.1', to: '5.18.0' }],
  [2, 14, 'prod-eu-west', { kind: 'updated', key: 'grafana', name: 'Grafana', compare: 'version', from: '11.6.1', to: '12.0.2' }],
  [3, 10, 'edge-site-02', { kind: 'added', key: 'compliance-operator', name: 'Compliance Operator', compare: 'version', to: '1.5.1' }],
  [3, 9, 'prod-us-east', { kind: 'updated', key: 'vm-running', name: 'VMs running', compare: 'info', from: '19', to: '21' }],
  [4, 18, 'edge-site-01', { kind: 'recovered' }],
  [4, 12, 'edge-site-01', { kind: 'unreachable', to: 'dial tcp: i/o timeout' }],
  [5, 13, 'prod-eu-central', { kind: 'updated', key: 'openshift', name: 'OpenShift', compare: 'version', from: '4.17.6', to: '4.17.9' }],
  [5, 13, 'prod-eu-west', { kind: 'updated', key: 'openshift', name: 'OpenShift', compare: 'version', from: '4.17.6', to: '4.17.9' }],
  [6, 11, 'stage-eu-central', { kind: 'updated', key: 'mcp-worker', name: 'MCP: worker', compare: 'match', from: 'Updated', to: 'Updating (3/9)' }],
  [7, 15, 'hub-eu-central', { kind: 'updated', key: 'grafana', name: 'Grafana', compare: 'version', from: '11.5.2', to: '12.0.2' }],
  [8, 10, 'prod-eu-west', { kind: 'updated', key: 'vm-running', name: 'VMs running', compare: 'info', from: '29', to: '30' }],
  [9, 17, 'prod-us-east', { kind: 'removed', key: 'web-terminal', name: 'Web Terminal', compare: 'version', from: '1.10.1' }],
  [11, 9, 'edge-site-02', { kind: 'joined' }],
  [12, 14, 'prod-eu-central', { kind: 'updated', key: 'openshift-pipelines-operator-rh', name: 'Red Hat OpenShift Pipelines', compare: 'version', from: '1.14.5', to: '1.17.1' }],
  [14, 16, 'prod-us-east', { kind: 'updated', key: 'cert-api', name: 'API', compare: 'expiry', from: '2026-06-28', to: '2027-06-25' }],
  [17, 11, 'prod-us-east', { kind: 'updated', key: 'vm-total', name: 'Virtual machines', compare: 'info', from: '23', to: '24' }],
  [19, 13, 'stage-eu-central', { kind: 'updated', key: 'openshift', name: 'OpenShift', compare: 'version', from: '4.16.18', to: '4.16.21' }],
  [23, 10, 'dev-eu-central', { kind: 'added', key: 'openshift-cert-manager-operator', name: 'cert-manager Operator', compare: 'version', to: '1.14.5' }],
]

function feed(now: number): Change[] {
  return FEED.map(([daysAgo, hour, cluster, rest]) => {
    const t = new Date(now - daysAgo * DAY)
    t.setHours(hour, between(0, 59), 0, 0)
    return { time: t.toISOString(), cluster, ...rest }
  }).sort((a, b) => b.time.localeCompare(a.time))
}

const isCounter = (c: Change) => c.compare === 'info'

export function mockChanges(q: {
  from?: string
  to?: string
  cluster?: string
  limit?: number
  counters: boolean
}): { changes: Change[]; hiddenCounters: number } {
  reseed()
  let changes = feed(Date.now())
  if (q.from) changes = changes.filter((c) => c.time >= q.from!)
  if (q.to) changes = changes.filter((c) => c.time <= q.to!)
  if (q.cluster) changes = changes.filter((c) => c.cluster === q.cluster)

  // Counters are dropped before the limit, the way the server does it, or a busy
  // day of counter churn spends the whole page on rows nobody will see.
  let hiddenCounters = 0
  if (!q.counters) {
    hiddenCounters = changes.filter(isCounter).length
    changes = changes.filter((c) => !isCounter(c))
  }
  return { changes: changes.slice(0, q.limit ?? 200), hiddenCounters }
}

export function mockCalendar(): { days: ChangeDay[]; first: string; last: string } {
  reseed()
  const now = Date.now()
  const all = feed(now)
  const byDay = new Map<string, { count: number; counters: number; clusters: Set<string> }>()
  for (const c of all) {
    const d = new Date(c.time)
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
    const day = byDay.get(key) ?? { count: 0, counters: 0, clusters: new Set<string>() }
    day.count++
    if (isCounter(c)) day.counters++
    day.clusters.add(c.cluster)
    byDay.set(key, day)
  }
  return {
    days: [...byDay.entries()]
      .map(([date, d]) => ({ date, count: d.count, counters: d.counters, clusters: d.clusters.size }))
      .sort((a, b) => a.date.localeCompare(b.date)),
    first: new Date(now - 30 * DAY).toISOString(),
    last: new Date(now).toISOString(),
  }
}

export const mockUser = { user: 'demo', email: 'demo@example.com' }

// CSV export, matching the column order of internal/api's exporter so the mock
// does not leave a dead button in the Actions menu.
export function mockCSV(): string {
  const m = mockMatrix()
  const head = ['component', 'key', 'group', 'compare', 'reference', ...m.clusters.map((c) => c.name)]
  const lines = [head.join(',')]
  for (const r of m.rows) {
    const cells = m.clusters.map((c) => r.cells[c.name]?.version ?? '')
    lines.push([r.name, r.key, r.group, r.compare, r.leader, ...cells].map(csvField).join(','))
  }
  return lines.join('\n') + '\n'
}

const csvField = (v: string) => (/[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)

// Timeline history for the mock fleet.
//
// The real endpoint reads it back from stored snapshots. Here it is generated
// backwards from the value the matrix shows now: counts drift a little, take the
// odd step when a node pool or a VM batch landed, and a cluster that joined
// inside the window starts late. That is what the drawing has to cope with, so
// that is what the fixture provides.
const JOINED_DAYS_AGO: Record<string, number> = { 'edge-site-02': 11 }

interface MockTimelinePoint {
  t: string
  version: string
  extra?: Record<string, string>
}

// walkBack produces one cluster's series, oldest first, ending on the value the
// matrix shows now.
//
// A count is a step function, not a wobble: a node pool is added on a Tuesday and
// the number holds for a fortnight. So the series starts flat at the current
// value, then a couple of moments are picked where it stepped up, and everything
// before each of those is lowered. Drifting a little on every boundary produced a
// sawtooth that no fleet has ever looked like.
function walkBack(current: number, count: number, drift: number): number[] {
  const values: number[] = new Array(count).fill(current)
  const events = Math.floor(rand() * 3) // nothing happened, or one or two things did
  for (let e = 0; e < events; e++) {
    const at = 1 + Math.floor(rand() * (count - 1)) // never the oldest boundary
    const delta = Math.max(1, Math.round(values[at] * drift * (0.5 + rand())))
    for (let i = 0; i < at; i++) values[i] = Math.max(1, values[i] - delta)
  }
  return values
}

export function mockTimeline(keys: string[], days: number, at?: string): unknown {
  reseed()
  const step = { 1: 1, 2: 2, 5: 4, 7: 6, 14: 12, 30: 24 }[days] ?? 6
  const to = at ? Date.parse(at) : Date.now()
  const from = to - days * DAY
  const boundaries: number[] = []
  for (let t = to; t > from; t -= step * 60 * MINUTE) boundaries.unshift(t)

  const matrix = mockMatrix(at)
  const byKey = new Map(matrix.rows.map((r) => [r.key, r]))

  const rows = keys.map((key) => {
    const row = byKey.get(key)
    const series: { cluster: string; points: MockTimelinePoint[] }[] = []
    if (!row) return { key, name: key, series }

    for (const cluster of matrix.clusters) {
      const cell = row.cells[cluster.name]
      if (!cell || cell.state === 'not_installed' || !cell.version) continue

      const joined = JOINED_DAYS_AGO[cluster.name]
      const startsAt = joined === undefined ? from : to - joined * DAY
      const own = boundaries.filter((t) => t >= startsAt)
      if (own.length === 0) continue

      // A volume row carries its two numbers in extra, and the total row only in
      // its display value, so read both the way the UI does.
      const pairText = cell.version.match(/^(\d+)\s*PVC\s*\/\s*(\d+)\s*PV$/)
      const pvc = cell.extra?.pvc ? Number(cell.extra.pvc) : pairText ? Number(pairText[1]) : null
      const pv = cell.extra?.pv ? Number(cell.extra.pv) : pairText ? Number(pairText[2]) : null
      if (pvc !== null && pv !== null) {
        const claims = walkBack(pvc, own.length, 0.12)
        const volumes = walkBack(pv, own.length, 0.12)
        series.push({
          cluster: cluster.name,
          points: own.map((t, i) => ({
            t: new Date(t).toISOString(),
            version: `${claims[i]} PVC / ${volumes[i]} PV`,
            extra: { pvc: String(claims[i]), pv: String(volumes[i]) },
          })),
        })
        continue
      }
      if (!/^\d+$/.test(cell.version)) continue // a version or a dash has no line
      const counts = walkBack(Number(cell.version), own.length, 0.3)
      series.push({
        cluster: cluster.name,
        points: own.map((t, i) => ({ t: new Date(t).toISOString(), version: String(counts[i]) })),
      })
    }
    return { key, name: row.name, series }
  })

  return {
    from: new Date(from).toISOString(),
    to: new Date(to).toISOString(),
    days,
    step: `${step}h0m0s`,
    rows,
    // The fixture always has a month, so only the longest window is short of it.
    stale: days > 30,
  }
}
