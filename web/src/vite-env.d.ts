/// <reference types="vite/client" />

// Brings in Vite's ambient module declarations (`*.css`, `?raw`, `?url`, …) and
// the `import.meta.env` types. TypeScript 6 reports side-effect imports of
// untyped modules as TS2882, so the stylesheet imports in src/components/*.ts
// need these declarations in the program.
