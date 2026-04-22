/* eslint-disable @typescript-eslint/no-unused-vars */
import type { MigrateUpArgs, MigrateDownArgs } from '@payloadcms/db-postgres'
import { sql } from '@payloadcms/db-postgres'

export async function up({ db, payload, req }: MigrateUpArgs): Promise<void> {
  await db.execute(sql`
    ALTER TABLE "posts" ADD COLUMN "show_on_kaitu" boolean DEFAULT true NOT NULL;
    ALTER TABLE "posts" ADD COLUMN "show_on_overleap" boolean DEFAULT true NOT NULL;
    ALTER TABLE "_posts_v" ADD COLUMN "version_show_on_kaitu" boolean DEFAULT true NOT NULL;
    ALTER TABLE "_posts_v" ADD COLUMN "version_show_on_overleap" boolean DEFAULT true NOT NULL;
  `)
}

export async function down({ db, payload, req }: MigrateDownArgs): Promise<void> {
  await db.execute(sql`
    ALTER TABLE "posts" DROP COLUMN "show_on_kaitu";
    ALTER TABLE "posts" DROP COLUMN "show_on_overleap";
    ALTER TABLE "_posts_v" DROP COLUMN "version_show_on_kaitu";
    ALTER TABLE "_posts_v" DROP COLUMN "version_show_on_overleap";
  `)
}
