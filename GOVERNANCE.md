# GOVERNANCE.md

## License

- Core (server, evaluator, migrate CLI, OpenFeature provider, Go/TS/Python SDKs): **MIT** — see `LICENSE`.
- Commercial hosted control-plane extras (SSO/SAML, multi-tenant, audit pipeline): **BSL 1.1 / ELv2** — see `LICENSE-CLOUD` (template stub until counsel reviews it).

Rationale: MIT-only cores that crossed $100k ARR got stripped or relicensed by hyperscalers. The BSL/ELv2 layer is a 12–18 month moat clock; spend it deliberately.

## Contributions

- **DCO only.** Every commit carries `Signed-off-by` (`git commit -s`). No CLA, ever. Any surprise CLA request is a governance violation and gets rejected in the PR.
- Contributions land in the MIT core only. The commercial layer accepts no outside contributions.
- No copyright assignment, no dual-licensing demands on contributors.

## License changes

Changing any license (core or commercial) requires all three:

1. **4/4 maintainer consensus** of the governance council.
2. **30-day public notice** before the change lands.
3. A written **migration path** for existing self-hosters.

Maintainer seats: (1) founder, (2)–(4) to be filled by the first two sustained external contributors plus one community advocate. **Until all four seats are filled, no license change may proceed at all.** No relicensing under acquisition pressure without the same process.

## Security

- Private report → 90-day fix window → coordinated release. `security.md` lands with the first public release.
