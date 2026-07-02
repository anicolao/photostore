# 001 Dashboard

This scenario verifies the first local operator workflow:

1. Load an initialized empty Photostore store.
2. Confirm the dashboard empty states render.
3. Add a temporary source root through the browser.
4. Start a source scan from the browser.
5. Confirm the completed scan report shows acquired files and retained duplicate
   bytes.

The scenario uses temporary fixture files with `.JPG` and `.jpeg` extensions.
The fixture bytes are intentionally tiny because the ingestion MVP trusts file
extensions rather than decoding image pixels.
