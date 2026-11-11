# Architecture

* [The pull model](pull-model.md) - One hub pod reads every cluster. No agents run on the joined clusters.
* [The scrape cycle](scrape-cycle.md) - How one scrape of the fleet runs, and what happens when a cluster fails.
* [How drift is decided](drift-model.md) - The fleet leader is the baseline. Clusters behind it shade red.
* [Access and security](security.md) - Who holds which rights, on the hub and on each joined cluster.
