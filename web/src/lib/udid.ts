export const getOrGenerateUdid = (): string => {
    let udid = localStorage.getItem('admin-udid');
    if (!udid) {
        udid = crypto.randomUUID();
        localStorage.setItem('admin-udid', udid);
    }
    return udid;
}; 