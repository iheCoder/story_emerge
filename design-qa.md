# Design QA

## Comparison Target

- Source visual truth: `/var/folders/vq/x5tzf0hs591_gh2c6tzkvnkm0000gn/T/codex-clipboard-912c307f-47fb-4dd1-a3d4-6567c255a484.png`
- Source pixels: `1586 x 992`
- Final implementation: `http://127.0.0.1:8787/#/create`
- Final implementation screenshot: `.playwright-cli/page-2026-08-30T05-21-55-100Z.png`
- Side-by-side comparison: `.playwright-cli/design-comparison-final.png`
- CSS viewport: `1586 x 992`; device scale factor: `1`
- State: creation page with the supplied inspiration and medium length selected

## Full-view Comparison Evidence

The source and implementation were placed in one equal-size side-by-side image before review. Both use the same dominant composition: high-contrast editorial headline at upper left, open bordered inspiration field below, a large orbital Story Core occupying the right visual field, a restrained length selector along the lower edge, and a black circular creation action connected to the system visually.

The implementation intentionally adds the required `创造 / 书架` navigation and uses a live Canvas Story Core instead of rasterizing the reference. Its nodes respond to input length and move along semantic story orbits. The result preserves the source's black, white, electric-blue, thin-line and geometric language without reproducing the reference as a static image.

## Focused Evidence

- Desktop reader: `.playwright-cli/page-2026-08-30T05-10-48-743Z.png`, viewport `1440 x 900`.
- Mobile creation: `.playwright-cli/page-2026-08-30T05-18-54-305Z.png`, viewport `390 x 844`.
- Mobile reader: `.playwright-cli/page-2026-08-30T05-13-44-756Z.png`, viewport `390 x 844`.
- Desktop library: `.playwright-cli/page-2026-08-30T05-10-02-463Z.png`, viewport `1440 x 900`.
- Mobile library: `.playwright-cli/page-2026-08-30T05-14-49-151Z.png`, viewport `390 x 844`.

Focused captures were required because the reference only defines the creation-page art direction, while the request also requires bookshelf and reader behavior. These captures verify the extended product states without pretending they have a pixel-identical source screen.

## Fidelity Surfaces

- Fonts and typography: high-contrast Songti-style Chinese display and reading faces are paired with the system sans UI face. Headline, chapter title, navigation and body hierarchy remain distinct with zero negative letter spacing. No clipping or button wrapping was observed.
- Spacing and layout rhythm: the creation page preserves the reference's asymmetry and large negative space. The reader uses peripheral space for a chapter spine and reading-progress core while keeping prose within a comfortable `760px` maximum column.
- Colors and tokens: near-white paper, true black, cool gray and electric blue dominate. Yellow, green and orange only identify Story Core nodes. No purple, large gradient, glass-card or dashboard treatment remains.
- Image and visual fidelity: the reference's geometric visual is implemented as a resolution-independent interactive Canvas system. It remained nonblank at desktop and mobile sizes; bookshelf covers reuse the same deterministic visual language.
- Copy and content: creation still asks only for inspiration and length. Agent-generated titles, chapter titles, progress and reader-facing generation states are surfaced without model or token terminology.

## Interaction Evidence

- Five existing disk novels appeared after server restart with generated titles and progress.
- Opening a remembered story after reading Chapter 2 restored `#/read/story-mtf9hlws-1/2`.
- On Chapter 1, `上一章` had `hidden=true` and computed `display:none`.
- On Chapter 1, the generated next chapter was enabled, titled `旧名回响`, and successfully opened Chapter 2.
- On the latest generated chapter, ordinary next was hidden and `生成下一章` was visible; it was not clicked, so no model call was made.
- Desktop and mobile had no horizontal document overflow.
- Browser console: `0` errors and `0` warnings.

## Comparison History

### Iteration 1

- P2: the mobile circular creation action fell below the initial `390 x 844` viewport.
  - Fix: moved the action into the mobile Story Core region and reduced it to a stable `94 x 94` footprint.
  - Post-fix evidence: `.playwright-cli/page-2026-08-30T05-18-54-305Z.png` shows it fully visible without covering the input or length controls.
- P2: the bookshelf top status briefly showed `0 个故事` before the asynchronous list finished.
  - Fix: refresh the status after rendering the returned library.
  - Post-fix evidence: browser state reported `5 个故事`, five book entries and no overflow.
- P2: an already-open browser could retain an older embedded stylesheet after server restart.
  - Fix: static assets now return `Cache-Control: no-store`.

### Iteration 2

No remaining P0, P1 or P2 findings were observed in the equal-size desktop comparison, mobile captures, navigation-state checks or console inspection.

## Follow-up Polish

- P3: exact Songti glyph metrics vary slightly by operating-system font availability; the current macOS target matches the intended editorial character.
- P3: Story Core orbit positions deliberately vary over time, so screenshots preserve the design language rather than exact node coordinates.

final result: passed
