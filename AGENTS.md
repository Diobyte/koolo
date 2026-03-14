# agents.md

## Role

Expert Go maintainer for **koolo** — a singleplayer Diablo II Resurrected: Reign of The Warlock bot. Be conservative. Prioritize correctness and safety over style.

---

## Rules

- All changes must compile, be `gofmt`-formatted, and match existing conventions.
- No new external dependencies unless requested.
- No new tests unless requested.
- Behavior changes must be explicitly intended and documented.
- When in doubt, don't change it — document the concern instead.
- Prefer early returns, reduce nesting, keep changes minimal.
- Only extract helpers when logic is reused in multiple places.

---

## Codebase Conventions

### Context Access (goroutine-scoped global)

```go
ctx := context.Get()           // goroutine-specific, mutex-protected
ctx.Data.PlayerUnit            // current player state
ctx.Data.Monsters              // visible monsters
ctx.Data.Inventory             // items
ctx.RefreshGameData()          // re-read from memory
ctx.Logger.Info("message")     // logger access
```

`context.Get()` uses goroutine ID internally. Never cache the context across goroutine boundaries.

### Logging — `log/slog` structured style

```go
ctx.Logger.Info("Starting run", slog.String("area", area.Name), slog.Int("attempt", n))
ctx.Logger.Debug("Monster position", slog.Any("pos", monster.Position))
ctx.Logger.Error("Failed to find target")
```

- Use `slog.String()`, `slog.Int()`, `slog.Duration()`, `slog.Any()` for attributes.
- Log at early returns, retries, fallbacks, and state changes.
- No logs in tight loops unless debug-guarded.

### Error Handling — standard Go

```go
return fmt.Errorf("error creating log directory: %w", err)  // wrap with %w
if errors.Is(err, os.ErrNotExist) { ... }                   // check with errors.Is
```

No wrapping libraries. Keep it simple and idiomatic.

### Actions & Steps

```go
// High-level actions compose steps
step.MoveTo(position, step.WithDistanceToFinish(10))
step.PrimaryAttack(targetID, numAttacks, standStill, opts...)
step.SecondaryAttack(skill.Blizzard, targetID, 1, attackOpts)
step.OpenInventory()
step.CloseAllMenus()
```

Functional options pattern: `type AttackOption func(*attackSettings)`.

### Config Access — mutex-protected

```go
config.Koolo       // global config, guarded by sync.RWMutex
config.Characters  // per-character config map
```

Always respect mutex when reading/writing config.

### Concurrency

- `sync.Mutex` / `sync.RWMutex` for shared state
- `atomic.Bool` for simple flags (e.g., `IsAllocatingStatsOrSkills`)
- `errgroup` for coordinated goroutines
- Watch for goroutine-scoped context when spawning new goroutines

### Key External Types (from `d2go`)

`data.UnitID`, `data.Position`, `area.ID`, `skill.ID`, `stat.ID`, `data.Monster`

### Naming

- Files: `snake_case.go`
- Types/constants: `PascalCase`
- Exported functions: `PascalCase`
- Unexported: `camelCase`

---

## Commit Messages

Conventional Commits: `type(scope): imperative summary`

