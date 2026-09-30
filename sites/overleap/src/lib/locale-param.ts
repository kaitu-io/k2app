import { notFound } from 'next/navigation';
import { isLocale, type Locale } from './site';

/** Route params arrive as `string`; narrow to a served Locale or 404. */
export async function localeOf(params: Promise<{ locale: string }>): Promise<Locale> {
  const { locale } = await params;
  if (!isLocale(locale)) notFound();
  return locale;
}
