// Package inventory owns current-EXE backpack, quickbar, avatar inventory,
// personal cargo, account-shared crystal/soul slots, item movement, sorting,
// deletion, selling, repair, and stackable-use commands.
//
// Mutations are repository-backed and fail closed when the request layout,
// ownership boundary, transaction, PVF contract, or required refresh body is
// not proved. Equipment-list endpoints are delegated to package equip.
package inventory
