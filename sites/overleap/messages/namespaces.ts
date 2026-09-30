/**
 * Every message namespace. Each is one file per locale: messages/<locale>/<ns>.json,
 * and every locale must carry every namespace with the same keys
 * (tests/messages-parity.test.ts). Add the name here when adding a file.
 */
export const NAMESPACES = ['common', 'nav', 'home', 'auth', 'account', 'errors', 'legal', 'k2'] as const;
export type Namespace = (typeof NAMESPACES)[number];
