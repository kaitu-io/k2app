import * as migration_20260422_085038_initial from './20260422_085038_initial';

export const migrations = [
  {
    up: migration_20260422_085038_initial.up,
    down: migration_20260422_085038_initial.down,
    name: '20260422_085038_initial'
  },
];
