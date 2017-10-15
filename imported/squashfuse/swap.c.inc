void sqfs_swapin_super_block(struct squashfs_super_block *s){
sqfs_swapin32_internal(&s->s_magic);
sqfs_swapin32_internal(&s->inodes);
sqfs_swapin32_internal(&s->mkfs_time);
sqfs_swapin32_internal(&s->block_size);
sqfs_swapin32_internal(&s->fragments);
sqfs_swapin16_internal(&s->compression);
sqfs_swapin16_internal(&s->block_log);
sqfs_swapin16_internal(&s->flags);
sqfs_swapin16_internal(&s->no_ids);
sqfs_swapin16_internal(&s->s_major);
sqfs_swapin16_internal(&s->s_minor);
sqfs_swapin64_internal(&s->root_inode);
sqfs_swapin64_internal(&s->bytes_used);
sqfs_swapin64_internal(&s->id_table_start);
sqfs_swapin64_internal(&s->xattr_id_table_start);
sqfs_swapin64_internal(&s->inode_table_start);
sqfs_swapin64_internal(&s->directory_table_start);
sqfs_swapin64_internal(&s->fragment_table_start);
sqfs_swapin64_internal(&s->lookup_table_start);
}
void sqfs_swapin_dir_index(struct squashfs_dir_index *s){
sqfs_swapin32_internal(&s->index);
sqfs_swapin32_internal(&s->start_block);
sqfs_swapin32_internal(&s->size);
}
void sqfs_swapin_base_inode(struct squashfs_base_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
}
void sqfs_swapin_ipc_inode(struct squashfs_ipc_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->nlink);
}
void sqfs_swapin_lipc_inode(struct squashfs_lipc_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->nlink);
sqfs_swapin32_internal(&s->xattr);
}
void sqfs_swapin_dev_inode(struct squashfs_dev_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->nlink);
sqfs_swapin32_internal(&s->rdev);
}
void sqfs_swapin_ldev_inode(struct squashfs_ldev_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->nlink);
sqfs_swapin32_internal(&s->rdev);
sqfs_swapin32_internal(&s->xattr);
}
void sqfs_swapin_symlink_inode(struct squashfs_symlink_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->nlink);
sqfs_swapin32_internal(&s->symlink_size);
}
void sqfs_swapin_reg_inode(struct squashfs_reg_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->start_block);
sqfs_swapin32_internal(&s->fragment);
sqfs_swapin32_internal(&s->offset);
sqfs_swapin32_internal(&s->file_size);
}
void sqfs_swapin_lreg_inode(struct squashfs_lreg_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin64_internal(&s->start_block);
sqfs_swapin64_internal(&s->file_size);
sqfs_swapin64_internal(&s->sparse);
sqfs_swapin32_internal(&s->nlink);
sqfs_swapin32_internal(&s->fragment);
sqfs_swapin32_internal(&s->offset);
sqfs_swapin32_internal(&s->xattr);
}
void sqfs_swapin_dir_inode(struct squashfs_dir_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->start_block);
sqfs_swapin32_internal(&s->nlink);
sqfs_swapin16_internal(&s->file_size);
sqfs_swapin16_internal(&s->offset);
sqfs_swapin32_internal(&s->parent_inode);
}
void sqfs_swapin_ldir_inode(struct squashfs_ldir_inode *s){
sqfs_swapin16_internal(&s->inode_type);
sqfs_swapin16_internal(&s->mode);
sqfs_swapin16_internal(&s->uid);
sqfs_swapin16_internal(&s->guid);
sqfs_swapin32_internal(&s->mtime);
sqfs_swapin32_internal(&s->inode_number);
sqfs_swapin32_internal(&s->nlink);
sqfs_swapin32_internal(&s->file_size);
sqfs_swapin32_internal(&s->start_block);
sqfs_swapin32_internal(&s->parent_inode);
sqfs_swapin16_internal(&s->i_count);
sqfs_swapin16_internal(&s->offset);
sqfs_swapin32_internal(&s->xattr);
}
void sqfs_swapin_dir_entry(struct squashfs_dir_entry *s){
sqfs_swapin16_internal(&s->offset);
sqfs_swapin16_internal(&s->inode_number);
sqfs_swapin16_internal(&s->type);
sqfs_swapin16_internal(&s->size);
}
void sqfs_swapin_dir_header(struct squashfs_dir_header *s){
sqfs_swapin32_internal(&s->count);
sqfs_swapin32_internal(&s->start_block);
sqfs_swapin32_internal(&s->inode_number);
}
void sqfs_swapin_fragment_entry(struct squashfs_fragment_entry *s){
sqfs_swapin64_internal(&s->start_block);
sqfs_swapin32_internal(&s->size);
}
void sqfs_swapin_xattr_entry(struct squashfs_xattr_entry *s){
sqfs_swapin16_internal(&s->type);
sqfs_swapin16_internal(&s->size);
}
void sqfs_swapin_xattr_val(struct squashfs_xattr_val *s){
sqfs_swapin32_internal(&s->vsize);
}
void sqfs_swapin_xattr_id(struct squashfs_xattr_id *s){
sqfs_swapin64_internal(&s->xattr);
sqfs_swapin32_internal(&s->count);
sqfs_swapin32_internal(&s->size);
}
void sqfs_swapin_xattr_id_table(struct squashfs_xattr_id_table *s){
sqfs_swapin64_internal(&s->xattr_table_start);
sqfs_swapin32_internal(&s->xattr_ids);
sqfs_swapin32_internal(&s->unused);
}
