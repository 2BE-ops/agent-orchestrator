# Composer model search review evidence

Captured from the actual `ChatComposer` and `TurnSettingsBar` components in an isolated Chromium renderer fixture with a deterministic 100-model catalog. The surrounding title and catalog note belong to the fixture. No live provider or daemon data was used.

The before capture uses `109c11ada`; the after captures use `2b668e832`. The viewport is 1100 by 720 pixels. A temporary Playwright capture test asserted filtering, selection, and query preservation while recording.

## Recording

[Watch or download the MP4](model-search-walkthrough.mp4).

![Recorded walkthrough of model search and keyboard navigation](model-search-walkthrough.gif)

The recording shows the original scroll list, searching for Model 99, returning to the query with Arrow Up and Shift+Tab, selecting the model, filtering ACP models by provider, and searching the standalone ACP picker.

## Screenshots

- [Before: native model list without search](before-native.png)
- [After: full native catalog with search and the type-to-narrow hint](after-native-catalog.png)
- [After: native search narrowed to Model 99](after-native-search.png)
- [After: ACP catalog filtered by provider](after-acp-provider.png)
- [After: fuzzy search in the standalone ACP picker](after-acp-standalone.png)

The capture test passed. Composition handling is covered by the committed component regression tests; the recording does not simulate an operating-system input method.
