import type { ZxcvbnResult } from '@zxcvbn-ts/core';

export const PASSWORD_MIN_LENGTH = 10;
export const PASSWORD_MIN_SCORE = 3;

export interface StrengthResult {
  score: 0 | 1 | 2 | 3 | 4;
  isValid: boolean;
  tooShort: boolean;
}

let zxcvbnPromise: Promise<typeof import('@zxcvbn-ts/core').zxcvbn> | null = null;

// Lazy-load zxcvbn + language packs so the cold-start bundle stays small —
// PasswordDialog opens on demand, not on app boot.
async function loadZxcvbn() {
  if (!zxcvbnPromise) {
    zxcvbnPromise = (async () => {
      const [core, common, en] = await Promise.all([
        import('@zxcvbn-ts/core'),
        import('@zxcvbn-ts/language-common'),
        import('@zxcvbn-ts/language-en'),
      ]);
      core.zxcvbnOptions.setOptions({
        translations: en.translations,
        dictionary: { ...common.dictionary, ...en.dictionary },
        graphs: common.adjacencyGraphs,
      });
      return core.zxcvbn;
    })();
  }
  return zxcvbnPromise;
}

export async function checkPasswordStrength(
  password: string,
  userInputs: string[] = [],
): Promise<StrengthResult> {
  if (!password) {
    return { score: 0, isValid: false, tooShort: true };
  }
  const tooShort = password.length < PASSWORD_MIN_LENGTH;
  const zxcvbn = await loadZxcvbn();
  const result: ZxcvbnResult = zxcvbn(password, userInputs);
  const score = result.score as StrengthResult['score'];
  return {
    score,
    tooShort,
    isValid: !tooShort && score >= PASSWORD_MIN_SCORE,
  };
}
