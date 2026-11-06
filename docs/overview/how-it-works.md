# How CardinalityBudget works

Mermaid diagrams. They render on GitHub and in VS Code (Markdown Preview Mermaid Support).

## 1. Big picture

```mermaid
flowchart LR
    PT[Platform team] -- writes --> CB[CardinalityBudget<br/>per namespace]
    OP[CardinalityBudget operator] -- watches --> CB
    OP -- "count series<br/>(PromQL, read only)" --> PROM[(Prometheus 3.x)]
    OP -- "status, Events" --> CB
    OP -- "drop rules<br/>(Enforce only)" --> SM[ServiceMonitors /<br/>PodMonitors]
    PO[Prometheus Operator] -- reads --> SM
    PO -- "new scrape config" --> PROM
    PROM -- scrapes --> APP[Team app pods]
    TEAM[Application team] -- "kubectl get/describe cb" --> CB
```

## 2. One reconcile

```mermaid
flowchart TD
    A([Budget changed, monitor changed,<br/>or RequeueAfter timer]) --> B{Budget being<br/>deleted?}
    B -- yes --> B1{Kill switch on?}
    B1 -- yes --> B2[Wait, retry later]
    B1 -- no --> B3[Remove every owned drop rule]
    B3 --> B4[Remove finalizer] --> Z([Done])
    B -- no --> C[Add finalizer if missing]
    C --> D{Resume annotation<br/>is new?}
    D -- yes --> D1[Clear pause, record token]
    D -- no --> E
    D1 --> E[Measure in Prometheus:<br/>ready? warmed up? warnings? fresh?]
    E --> F{Measurement valid<br/>and spec valid?}
    F -- no --> F1[Phase Unknown, Ready=False<br/>NO monitor writes]
    F1 --> P
    F -- yes --> G[Compute usage %, phase,<br/>topMetrics, exceededSince]
    G --> H{Mode}
    H -- Observe --> P
    H -- Warn --> H1[Compute plannedDrops] --> P
    H -- "Enforce, or drops left over" --> I[Enforcement step<br/>see chart 3]
    I --> P[Patch status, emit Events on change,<br/>update metrics]
    P --> Q(["RequeueAfter = min(measure interval,<br/>settle time left) ± jitter"])
```

## 3. Enforcement step (under a mutex, fresh read of the budget)

```mermaid
flowchart TD
    S([Start]) --> K{Kill switch<br/>--enforcement-paused?}
    K -- yes --> K1[Enforced=False/Paused<br/>no writes] --> END
    K -- no --> R[Maintain existing drops]
    R --> R1{Prometheus reloaded<br/>after last change?}
    R1 -- no --> PAUSE1[Pause: ConfigReloadFailed] --> END
    R1 -- yes --> R2{Settle period passed<br/>since last drop?}
    R2 -- yes --> R3{Series of dropped metric<br/>down by ≥ 90%?}
    R3 -- no --> PAUSE2[Pause: Ineffective] --> END
    R3 -- yes --> R4[Mark drop Effective]
    R2 -- no --> R5
    R4 --> R5{Our rule missing or<br/>changed on a monitor?}
    R5 -- yes --> PAUSE3[Mark Reverted, pause:<br/>never re-apply] --> END
    R5 -- no --> R6[Restore newly protected metrics now<br/>remove rules from deselected monitors<br/>add rules to new monitors]
    R6 --> A{Paused?}
    A -- yes --> END
    A -- no --> M{Mode Enforce and<br/>usage ≥ 100%?}
    M -- yes --> G{Grace period over,<br/>settle period over?}
    G -- no --> END
    G -- yes --> L{Under maxTotalDrops and<br/>--max-drops-per-hour?}
    L -- no --> L1[Enforced: LimitReached /<br/>RateLimited] --> END
    L -- yes --> D[Pick largest metrics:<br/>not protected, no ':'<br/>up to maxMetricsToDrop]
    D --> W[Append drop rule + annotation + label<br/>to every selected monitor<br/>optimistic lock; conflict → retry]
    W --> W1[Record enforcedDrops Pending,<br/>seriesAtDrop, Event Dropped] --> END
    M -- no --> U{Usage below warn threshold,<br/>or mode not Enforce?}
    U -- no --> END
    U -- yes --> U1{Settle period over?}
    U1 -- no --> END
    U1 -- yes --> U2[Restore newest drop first<br/>maxMetricsToDrop per step<br/>Event Restored] --> END
    END([Back to reconcile: patch status])
```

## 4. Life of one drop

```mermaid
stateDiagram-v2
    [*] --> Pending: rule added
    Pending --> Effective: after settle, ≥ 90% of series gone
    Pending --> Ineffective: after settle, < 90% gone (budget pauses)
    Pending --> Reverted: rule removed by someone (budget pauses)
    Effective --> Reverted: rule removed by someone (budget pauses)
    Effective --> [*]: restored (usage down, mode changed, protected, budget deleted)
    Ineffective --> [*]: restored
    Reverted --> [*]: cleaned up after resume
```

## 5. Phases

```mermaid
stateDiagram-v2
    [*] --> Unknown
    Unknown --> Healthy: valid measurement, below warn
    Healthy --> Warning: usage ≥ warnThresholdPercent
    Warning --> Exceeded: usage ≥ 100%
    Exceeded --> Enforcing: Enforce mode drops something
    Enforcing --> Exceeded: still over, waiting
    Exceeded --> Warning: usage falls
    Warning --> Healthy: usage falls
    Healthy --> Unknown: measurement invalid
    Warning --> Unknown: measurement invalid
    Exceeded --> Unknown: measurement invalid
    Enforcing --> Unknown: measurement invalid
```
