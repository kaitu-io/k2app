export const getOrGenerateUdid = async (): Promise<string> => {
    const STORAGE_KEY = 'admin-udid';
    let raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) {
        raw = crypto.randomUUID();
        localStorage.setItem(STORAGE_KEY, raw);
    }
    // Hash to uniform 32 hex chars (same format as native platforms)
    const data = new TextEncoder().encode(raw);
    const hashBuffer = await crypto.subtle.digest('SHA-256', data);
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    return hashArray.slice(0, 16).map(b => b.toString(16).padStart(2, '0')).join('');
};
