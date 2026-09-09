from events import page

def all_rows(rows, batch_size):
    return page(rows, batch_size)['items']
