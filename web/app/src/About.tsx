import {
  Button,
  Content,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Flex,
} from '@patternfly/react-core'
import { BugIcon, GithubIcon } from '@patternfly/react-icons'
import { APP_VERSION, ISSUES_URL, REPO_URL } from './project'

export function About() {
  return (
    <>
      <Content className="cc-prose">
        <p>
          Periscope is a read-only dashboard for a fleet of OpenShift clusters. It answers one
          question: <strong>what is out of date, and where?</strong> Every component it knows about
          is one row, every cluster is one column, and each cell is colored by how far that cluster
          has fallen behind the rest of the fleet.
        </p>

        <h2>What it compares</h2>
        <ul>
          <li>
            <strong>OpenShift release</strong> — the cluster version reported by the cluster version
            operator.
          </li>
          <li>
            <strong>Operators</strong> — the installed version of each operator, taken from its
            cluster service version.
          </li>
          <li>
            <strong>CSI drivers</strong> — the version of each managed storage driver.
          </li>
          <li>
            <strong>Configuration</strong> — values that are meant to be identical fleet-wide, such
            as the update channel. These are flagged when a cluster differs, rather than ranked.
          </li>
          <li>
            <strong>Certificates</strong> — days left before expiry, warning as the date gets close.
          </li>
          <li>
            <strong>Inventory</strong> — counts and details that are reported rather than ranked,
            such as total nodes, machine config pools, the default storage class, and virtual
            machines.
          </li>
        </ul>

        <h2>How it works</h2>
        <p>
          Each cluster joins by adding a labeled Secret that holds its API URL and a token for a
          read-only service account. Periscope scrapes those clusters on a schedule, stores each
          scrape locally, and serves the newest successful scrape per cluster. A cluster whose last
          scrape is old is marked <em>stale</em>, and one that failed carries the error, so a broken
          collector never looks like a healthy cluster.
        </p>
        <p>
          For versions, the drift baseline is the highest semver seen across the fleet, not a value
          you configure. The cell showing that version is the leader; the others get a darker red as
          the gap grows. Refresh triggers a scrape right away, and the matrix can be exported as CSV
          or JSON for reporting.
        </p>

        <h2>Custom groups</h2>
        <p>
          The row sections and the pages they appear on come from the <code>periscope-groups</code>{' '}
          ConfigMap. The <strong>Docs</strong> page lists every component key discovered across your
          clusters, which are the names to use when writing those groups.
        </p>

        <h2>Project</h2>
      </Content>

      <Flex gap={{ default: 'gapSm' }} className="cc-about-links">
        <Button
          variant="secondary"
          component="a"
          href={REPO_URL}
          target="_blank"
          rel="noreferrer"
          icon={<GithubIcon />}
        >
          Source on GitHub
        </Button>
        <Button
          variant="link"
          component="a"
          href={ISSUES_URL}
          target="_blank"
          rel="noreferrer"
          icon={<BugIcon />}
        >
          Report an issue
        </Button>
      </Flex>

      <DescriptionList isHorizontal isCompact className="cc-about-meta">
        <DescriptionListGroup>
          <DescriptionListTerm>Web UI version</DescriptionListTerm>
          <DescriptionListDescription>{APP_VERSION}</DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>License</DescriptionListTerm>
          <DescriptionListDescription>Apache License 2.0</DescriptionListDescription>
        </DescriptionListGroup>
      </DescriptionList>
    </>
  )
}
