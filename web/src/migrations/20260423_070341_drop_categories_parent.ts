/* eslint-disable @typescript-eslint/no-unused-vars */
import type { MigrateUpArgs, MigrateDownArgs } from '@payloadcms/db-postgres'
import { sql } from '@payloadcms/db-postgres'

export async function up({ db, payload, req }: MigrateUpArgs): Promise<void> {
  await db.execute(sql`
    ALTER TABLE "categories" DROP CONSTRAINT IF EXISTS "categories_parent_id_categories_id_fk";
    DROP INDEX IF EXISTS "categories_parent_idx";
    ALTER TABLE "categories" DROP COLUMN IF EXISTS "parent_id";
  `)
}

export async function down({ db, payload, req }: MigrateDownArgs): Promise<void> {
  await db.execute(sql`
    ALTER TABLE "categories" ADD COLUMN "parent_id" integer;
    ALTER TABLE "categories" ADD CONSTRAINT "categories_parent_id_categories_id_fk"
      FOREIGN KEY ("parent_id") REFERENCES "categories"("id") ON DELETE SET NULL;
    CREATE INDEX "categories_parent_idx" ON "categories" USING btree ("parent_id");
  `)
}
