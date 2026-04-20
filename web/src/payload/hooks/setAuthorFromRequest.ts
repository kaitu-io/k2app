import type { CollectionBeforeChangeHook } from 'payload'

export const setAuthorFromRequest: CollectionBeforeChangeHook = async ({
  data, req, operation,
}) => {
  if (operation === 'create' && req.user?.collection === 'admins') {
    data.author = req.user.id
  }
  return data
}
