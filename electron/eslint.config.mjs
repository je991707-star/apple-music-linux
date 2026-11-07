// ESLint flat config for the Electron app, renderer sources and tests.
// Run with `npm run lint`. Empty catch blocks and `_`-prefixed unused names are
// deliberate in this codebase (fire-and-forget calls, ignored parameters).
import js from '@eslint/js';
import globals from 'globals';

export default [
    { ignores: ['**/*-bundle.js', 'dist/**', 'node_modules/**'] },
    js.configs.recommended,
    {
        files: ['**/*.mjs', '**/*.js'],
        languageOptions: {
            ecmaVersion: 2023,
            sourceType: 'module',
            globals: { ...globals.browser, ...globals.node },
        },
    },
    {
        files: ['**/*.cjs'],
        languageOptions: { sourceType: 'commonjs', globals: { ...globals.node, ...globals.browser } },
    },
    {
        rules: {
            'no-empty': ['error', { allowEmptyCatch: true }],
            'no-unused-vars': ['error', {
                args: 'after-used', argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrors: 'none',
            }],
        },
    },
];
