# Agent Orchestrator Architecture Diagram

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "primaryColor": "#ffffff", "primaryTextColor": "#000000", "primaryBorderColor": "#000000", "secondaryColor": "#ffffff", "secondaryTextColor": "#000000", "secondaryBorderColor": "#000000", "tertiaryColor": "#ffffff", "tertiaryTextColor": "#000000", "tertiaryBorderColor": "#000000", "lineColor": "#000000", "clusterBkg": "#ffffff", "clusterBorder": "#000000", "edgeLabelBackground": "#ffffff", "fontFamily": "Inter, Arial, sans-serif", "fontSize": "22px"}, "themeCSS": ".edgePath .path { stroke: #000000 !important; stroke-width: 2px !important; } .flowchart-link { stroke: #000000 !important; stroke-width: 2px !important; } .node rect, .node polygon, .node path, .node circle, .cluster rect { stroke: #000000 !important; stroke-width: 2px !important; }", "flowchart": {"htmlLabels": true, "curve": "basis", "nodeSpacing": 40, "rankSpacing": 55, "useMaxWidth": false}}}%%
flowchart TB
    User(["Developer / Supervisor"])
    Phone(["Mobile client"])

    subgraph Clients["Client surfaces"]
        Renderer["Electron React renderer<br/>Kanban · Chat · Terminal · PRs · Reviews"]
        ElectronMain["Electron main process<br/>Daemon supervision · Browser targets"]
        CLI["ao CLI<br/>Thin HTTP client"]
        Mobile["React Native mobile UI<br/>Thin renderer"]
    end

    User --> Renderer
    User --> CLI
    Phone --> Mobile

    subgraph Network["Daemon access and security boundaries"]
        Loopback["Primary listener<br/>127.0.0.1<br/>Unauthenticated"]
        LAN["Optional Connect Mobile listener<br/>0.0.0.0 · default off<br/>Bearer password + per-source lockout"]
        Identity["GET /api/v1/identity<br/>Only unauthenticated LAN route"]
        LANGate["LAN route gate<br/>App API only<br/>No shutdown, telemetry,<br/>mobile-control, or browser API"]
        Router["Shared chi router<br/>Request ID · logging · timeout · CORS"]
        REST["REST API<br/>/api/v1/*"]
        SSE["SSE event stream<br/>Last-Event-ID replay"]
        Mux["Terminal WebSocket<br/>/mux"]
        Control["Loopback control routes<br/>health · readiness · shutdown"]
    end

    Renderer -->|"REST · SSE"| Loopback
    Renderer -->|"Terminal frames"| Loopback
    CLI -->|"REST"| Loopback
    Mobile -->|"Identity probe"| Identity
    Mobile -->|"Authenticated REST · SSE · WS"| LAN

    Loopback --> Router
    LAN --> LANGate
    Identity --> Router
    LANGate --> Router

    Router --> REST
    Router --> SSE
    Router --> Mux
    Router --> Control

    subgraph HTTP["HTTP controllers"]
        ProjectCtl["Projects"]
        SessionCtl["Sessions and orchestrators"]
        ChatCtl["Conversations · turns<br/>approvals · structured input"]
        PRCtl["Pull requests · merge<br/>resolve comments"]
        ReviewCtl["Agent reviews"]
        NotificationCtl["Notifications"]
        BrowserCtl["Browser and preview"]
        SettingsCtl["Agents · authentication<br/>usage · settings"]
    end

    REST --> ProjectCtl
    REST --> SessionCtl
    REST --> ChatCtl
    REST --> PRCtl
    REST --> ReviewCtl
    REST --> NotificationCtl
    REST --> BrowserCtl
    REST --> SettingsCtl

    subgraph Services["Controller-facing services"]
        ProjectSvc["Project service"]
        SessionSvc["Session service<br/>Read-model assembly"]
        ChatSvc["Chat service<br/>Durable conversation projection"]
        PRSvc["PR action service"]
        ReviewSvc["Review service"]
        NotificationSvc["Notification service"]
        BrowserSvc["Browser authority service"]
        AgentSvc["Agent readiness and auth"]
        Orchestrator["Project orchestrator<br/>Persistent planning context<br/>Worker delegation"]
        Derive["Derived display status<br/>Never persisted"]
    end

    ProjectCtl --> ProjectSvc
    SessionCtl --> SessionSvc
    SessionCtl --> Orchestrator
    ChatCtl --> ChatSvc
    PRCtl --> PRSvc
    ReviewCtl --> ReviewSvc
    NotificationCtl --> NotificationSvc
    BrowserCtl --> BrowserSvc
    SettingsCtl --> AgentSvc

    subgraph Core["Core command and lifecycle layer"]
        SessionMgr["Session manager<br/>Spawn · kill · restore · rollback<br/>cleanup · interface handoff"]
        Mode{"Committed session_mode<br/>Exactly one controller"}
        TUIController["TUI controller<br/>Agent terminal runtime"]
        ChatController["Native Chat controller<br/>Runtime-less provider protocol"]
        Handoff["Durable TUI ↔ Chat handoff<br/>Drain or interrupt<br/>Generation fencing · rollback"]
        Lifecycle["Lifecycle manager<br/>Canonical durable-fact reducer"]
        Messenger["Mode-aware messenger<br/>User input · CI/review nudges"]
        Guardrails["Termination guardrails<br/>Failed probe ≠ dead<br/>Protect dirty worktrees"]
    end

    SessionSvc --> SessionMgr
    Orchestrator -->|"Spawn / redirect workers"| SessionMgr
    ChatSvc --> ChatController
    SessionMgr --> Mode
    Mode -->|"mode = tui"| TUIController
    Mode -->|"mode = chat"| ChatController
    SessionMgr --> Handoff
    Handoff --> Mode
    SessionMgr --> Lifecycle
    Lifecycle --> Guardrails
    Lifecycle --> Messenger
    Messenger --> TUIController
    Messenger --> ChatController

    subgraph Ports["Port contracts"]
        AgentPort["Agent capability"]
        RuntimePort["Runtime capability"]
        WorkspacePort["Workspace capability"]
        ChatPort["Chat-driver capability"]
        SCMPort["SCM capability"]
        TrackerPort["Tracker capability"]
        StorePort["Store interfaces"]
    end

    TUIController --> AgentPort
    TUIController --> RuntimePort
    ChatController --> ChatPort
    SessionMgr --> WorkspacePort
    ProjectSvc --> StorePort
    SessionSvc --> StorePort
    ChatSvc --> StorePort
    PRSvc --> StorePort
    ReviewSvc --> StorePort
    Lifecycle --> StorePort

    subgraph Adapters["Replaceable adapters"]
        Agents["Agent adapters<br/>Codex · Claude Code · Cursor<br/>OpenCode · Aider · others"]
        Runtime["Runtime adapters<br/>tmux · macOS detached PTY<br/>Windows ConPTY"]
        Workspace["Workspace adapters<br/>Git worktree · scratch directory"]
        ChatDrivers["Native Chat drivers<br/>Codex app-server · ACP"]
        PersistentHosts["Authenticated detached<br/>per-session provider hosts"]
        GitHubAdapter["GitHub SCM adapter"]
        TrackerAdapter["GitHub tracker adapter"]
        ReviewerAdapters["Interactive reviewer adapters"]
    end

    AgentPort --> Agents
    RuntimePort --> Runtime
    WorkspacePort --> Workspace
    ChatPort --> PersistentHosts
    PersistentHosts --> ChatDrivers
    SCMPort --> GitHubAdapter
    TrackerPort --> TrackerAdapter
    ReviewSvc --> ReviewerAdapters

    subgraph Execution["Per-worker isolated execution"]
        Worktree["Dedicated branch + worktree<br/>or AO scratch workspace"]
        TerminalHost["Detached terminal host<br/>Survives UI/daemon replacement"]
        ProviderHost["Detached Chat provider process<br/>Conversation identity preserved"]
        AgentProcess["Coding-agent CLI"]
        Repo["Git repository"]
    end

    Workspace --> Worktree
    Worktree --> Repo
    Runtime --> TerminalHost
    TerminalHost --> AgentProcess
    Agents --> AgentProcess
    ChatDrivers --> ProviderHost
    ProviderHost --> AgentProcess
    AgentProcess --> Worktree

    subgraph Observation["Observation loops"]
        Activity["Agent hooks / Chat events<br/>active · idle · waiting · blocked"]
        Reaper["Runtime reaper<br/>Liveness observations"]
        SCMObserver["SCM observer<br/>PR · CI · reviews · conflicts"]
        Usage["Usage and readiness observers"]
    end

    AgentProcess -.-> Activity
    ChatController -.-> Activity
    Runtime -.-> Reaper
    GitHubAdapter -.-> SCMObserver
    AgentSvc -.-> Usage

    Activity --> Lifecycle
    Reaper --> Lifecycle
    SCMObserver --> Lifecycle
    SCMObserver --> PRSvc
    Usage --> StorePort

    subgraph Persistence["Durable state under ~/.ao"]
        DB[("SQLite<br/>projects · sessions · conversations<br/>turns · messages · transitions<br/>PRs · checks · comments<br/>notifications · usage")]
        Facts["Minimal durable session facts<br/>activity_state · is_terminated<br/>session_mode · handles<br/>controller generation · PR facts"]
        Triggers["SQLite triggers"]
        ChangeLog[("change_log<br/>Ordered CDC sequence")]
        Poller["CDC poller<br/>Watermark + decoding"]
        Broadcaster["In-process broadcaster"]
        NotifyHub["Notification hub"]
    end

    StorePort --> DB
    Lifecycle --> Facts
    Facts --> DB
    DB -->|"INSERT / UPDATE / DELETE"| Triggers
    Triggers -->|"Append"| ChangeLog
    ChangeLog --> Poller
    Poller --> Broadcaster
    Lifecycle --> NotificationSvc
    NotificationSvc --> NotifyHub

    DB -->|"Raw durable facts"| Derive
    Derive --> SessionSvc
    Broadcaster -->|"Session invalidation and replay"| SSE
    Broadcaster -->|"Terminal state events"| Mux
    NotifyHub --> SSE

    subgraph TerminalPath["Terminal data path — separate from CDC"]
        TerminalMgr["Terminal manager"]
        PTYStream["Raw PTY byte stream"]
    end

    Mux <--> TerminalMgr
    TerminalMgr <--> PTYStream
    PTYStream <--> Runtime

    subgraph BrowserBridge["Session-isolated browser runtime"]
        BrowserBroker["Daemon browser broker<br/>Authorization + correlation"]
        BrowserSocket["Authenticated local socket<br/>Not a remote-debugging port"]
        CDPMux["Electron CDP multiplexer"]
        WebView["Selected worker WebContentsView<br/>Isolated profile per worker"]
        Preview["Managed preview server"]
    end

    BrowserSvc --> BrowserBroker
    BrowserBroker <--> BrowserSocket
    BrowserSocket <--> ElectronMain
    ElectronMain --> CDPMux
    CDPMux <--> WebView
    BrowserSvc --> Preview
    Preview --> WebView

    ElectronMain <-->|"Supervisor liveness socket"| Control

    subgraph External["External systems"]
        GitHub["GitHub API<br/>PRs · checks · reviews · merge"]
        ProviderCLIs["Installed provider CLIs<br/>User-owned authentication"]
    end

    GitHubAdapter <--> GitHub
    TrackerAdapter <--> GitHub
    PRSvc --> GitHubAdapter
    ReviewerAdapters --> ProviderCLIs
    ChatDrivers --> ProviderCLIs

    SCMObserver -->|"Actionable CI/review/conflict feedback"| Messenger
    PRSvc -->|"Updated PR facts"| DB
    DB -->|"Derived Kanban state"| Derive
```

Solid arrows show commands or data flow. Dotted arrows show observations.

The central invariant is that every worker owns one isolated workspace and exactly one committed controller—TUI or Chat—while SQLite stores durable facts and the service layer derives user-facing status.

## Image exports

- [A4 landscape SVG](assets/architecture/agent-orchestrator-a4-landscape.svg) — recommended for zooming and printing.
- [A4 landscape PNG](assets/architecture/agent-orchestrator-a4-landscape.png) — high-resolution raster export.
