import * as migration_20260422_085038_initial from './20260422_085038_initial';
import * as migration_20260422_174603_add_post_brand_visibility from './20260422_174603_add_post_brand_visibility';

export const migrations = [
  {
    up: migration_20260422_085038_initial.up,
    down: migration_20260422_085038_initial.down,
    name: '20260422_085038_initial'
  },
  {
    up: migration_20260422_174603_add_post_brand_visibility.up,
    down: migration_20260422_174603_add_post_brand_visibility.down,
    name: '20260422_174603_add_post_brand_visibility'
  },
];
