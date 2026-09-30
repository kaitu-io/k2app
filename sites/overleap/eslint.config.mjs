import { dirname } from "path";
import { fileURLToPath } from "url";
import { FlatCompat } from "@eslint/eslintrc";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const compat = new FlatCompat({
  baseDirectory: __dirname,
});

const eslintConfig = [
  ...compat.extends("next/core-web-vitals", "next/typescript"),
  {
    rules: {
      // Internationalization rules - changed from 'error' to 'warn' for now
      'react/jsx-no-literals': 'warn',
      'no-restricted-imports': [
        'error',
        {
          name: 'next/navigation',
          importNames: ['redirect', 'permanentRedirect', 'useRouter', 'usePathname'],
          message: 'Please import from `@/i18n/routing` instead.'
        }
      ]
    }
  },
  // Exception for admin pages - they don't need i18n
  {
    files: ['src/app/(manager)/**/*', 'src/components/manager-sidebar.tsx'],
    rules: {
      'react/jsx-no-literals': 'off',
      'no-restricted-imports': 'off'
    }
  }
];

export default eslintConfig;
