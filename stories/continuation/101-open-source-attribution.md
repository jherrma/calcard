# Story 101: Open Source Attribution

## Title

Generate and Serve Open Source Attribution List

## Description

As a user, I want to view a list of all open-source libraries and packages used in both the backend and frontend of the application, so that I can comply with licensing requirements and understand the project's dependencies.

## Acceptance Criteria

### Backend License Generation

- [ ] A script or command-line tool to generate a list of all Go module dependencies.
- [ ] The list should include: name, version, license type (if detectable), and URL.
- [ ] The list should be generated automatically before or during the server build process.
- [ ] REST endpoint `GET /api/v1/about/open-source` (authenticated users) to return the generated list as JSON.

### Frontend License Generation

- [ ] A script or build step to generate a list of all NPM/Yarn dependencies.
- [ ] The list should be stored as a static JSON file (e.g., `src/assets/open-source.json`) during the build process.
- [ ] The generated list should include: package name, version, license type, and repository URL.

### Frontend Display

- [ ] A dedicated "Open Source Attribution" or "About" page in the web interface.
- [ ] The page should fetch and display the list of both backend and frontend dependencies.
- [ ] The UI should be searchable or filterable by library name.
- [ ] Clicking on a library should link to its source repository or license text.

## Technical Notes

### Backend Generation (Go)

The backend list can be generated using `go list -m all` or a tool like `go-licence-detector`.
The generated file could be embedded into the Go binary using `//go:embed`.

```go
type OpenSourcePackage struct {
    Name    string `json:"name"`
    Version string `json:"version"`
    License string `json:"license"`
    URL     string `json:"url"`
}
```

### Frontend Generation (Node/Vite)

Tools like `license-checker` or `rollup-plugin-license` can be used to scan `package.json` and generate the attribution file during the Vite build.

### Proposed Directory Structure

```
server/
└── internal/
    └── usecase/
        └── about/
            └── list_open_source.go
webinterface/
└── src/
    └── pages/
        └── About.tsx
```

## Definition of Done

- [ ] Backend generation script is integrated into the build process/Dockerfile.
- [ ] `GET /api/v1/about/open-source` returns correct backend dependency data.
- [ ] Frontend build process generates `open-source.json`.
- [ ] The web interface displays a complete attribution list with search functionality.
- [ ] All licenses are correctly attributed and linked.
