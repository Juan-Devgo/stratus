package service

// PackageCatalog es la única lista de aplicaciones sugeridas (REQ-5). La UI la lee de
// GET /api/prepare/catalog, así que agregar un paquete solo requiere cambiar esta línea.
// Cada entrada debe cumplir ValidatePackage.
var PackageCatalog = []string{"openssh-server", "haproxy", "nginx", "apache2", "curl", "htop", "git", "ufw"}
