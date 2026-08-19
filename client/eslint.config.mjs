// Fleet standard ESLint config — byte-identical in every repo.
//
// DO NOT EDIT IN ONE REPO. Change it in every repo at once; the byte-identity
// checker fails the build otherwise. It is shaped so it can collapse into a
// one-line import of @aurum-alpha/eslint-config later without any repo having
// to change anything else.
//
// Two linters on purpose. oxlint runs the fast syntactic subset on everything;
// eslint earns its runtime by doing the one thing oxlint cannot — type-aware
// analysis. If that ever stops being true, drop one.
//
// SEVERITY IS TIERED, deliberately:
//
//   error  the rules that catch bugs. A floating promise is a dropped error
//          path; a misused promise is an `if (somePromise)` that is always
//          truthy. These are defects, not opinions.
//
//   warn   the `no-unsafe-*` family and no-explicit-any. Measured across the
//          fleet these are ~69% of all findings — they describe how much `any`
//          the codebase carries, which is real debt but not a reason to block
//          unrelated work. Visible, counted, burned down deliberately.
//
// The eslint jobs additionally run warn_only while that debt exists. Flipping
// warn_only off is the ratchet; tightening these warns to error is the ratchet
// after that.
import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import prettier from "eslint-config-prettier";

export default tseslint.config(
  {
    // Build output and vendored code are not ours to lint.
    ignores: [
      "dist/**",
      "build/**",
      "coverage/**",
      "node_modules/**",
      "**/*.d.ts",
    ],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,

  // Type-aware analysis, enabled for the two rules that pay for it.
  {
    files: ["**/*.{ts,tsx,mts,cts}"],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "error",
    },
  },

  // The `any` debt. Warned, not errored — see the header.
  {
    files: ["**/*.{ts,tsx,mts,cts}"],
    rules: {
      "@typescript-eslint/no-explicit-any": "warn",
      "@typescript-eslint/no-unsafe-argument": "warn",
      "@typescript-eslint/no-unsafe-assignment": "warn",
      "@typescript-eslint/no-unsafe-call": "warn",
      "@typescript-eslint/no-unsafe-member-access": "warn",
      "@typescript-eslint/no-unsafe-return": "warn",
    },
  },

  // Config and script files sit outside the typed program.
  {
    files: ["**/*.{js,mjs,cjs}", "**/*.config.*"],
    ...tseslint.configs.disableTypeChecked,
  },

  {
    // Keyed on the extension, not on a directory: client-manager's front end is
    // a self-contained project whose sources sit at src/, and a path-based glob
    // would silently match nothing there. .tsx means React wherever it lives.
    files: ["**/*.tsx"],
    plugins: { "react-hooks": reactHooks, "react-refresh": reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": [
        "warn",
        { allowConstantExport: true },
      ],
    },
  },

  {
    rules: {
      // An underscore prefix is the documented way to say "required by the
      // signature, deliberately unused".
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
        },
      ],
    },
  },

  // Last, so prettier owns formatting and eslint never argues about it.
  prettier,
);
